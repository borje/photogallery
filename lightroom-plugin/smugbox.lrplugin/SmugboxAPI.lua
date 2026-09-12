--[[
HTTP client for the gallery backend's /api/publish endpoints.

Kept free of Lightroom UI and catalog objects so it can be read and reviewed
on its own. Every call returns:  ok (boolean), result (table on success,
error message string on failure), status (HTTP status or nil), body (decoded
JSON table if any).
]]

local LrHttp = import "LrHttp"
local LrPathUtils = import "LrPathUtils"
local LrTasks = import "LrTasks"

local json = require "dkjson"
local Util = require "Util"

local SmugboxAPI = {}
SmugboxAPI.__index = SmugboxAPI

local TIMEOUT = 30 -- seconds, per connection phase
local UPLOAD_TIMEOUT = 600

-- Seconds to wait before each retry. Long enough to sit out a container
-- redeploy, during which the reverse proxy answers for a backend that is not
-- there.
local RETRY_DELAYS = { 5, 15, 45, 90, 120 }

function SmugboxAPI.normalizeUrl(url)
	url = Util.trim(url)
	url = url:gsub("/+$", "")
	return url
end

function SmugboxAPI.new(serverUrl, apiKey)
	local self = setmetatable({}, SmugboxAPI)
	self.baseUrl = SmugboxAPI.normalizeUrl(serverUrl)
	self.apiKey = Util.trim(apiKey)
	return self
end

function SmugboxAPI:isConfigured()
	return self.baseUrl:match("^https?://") ~= nil and self.apiKey ~= ""
end

function SmugboxAPI:headers(withJsonBody)
	local h = {
		{ field = "Authorization", value = "Bearer " .. self.apiKey },
		{ field = "Accept", value = "application/json" },
	}
	if withJsonBody then
		table.insert(h, { field = "Content-Type", value = "application/json" })
	end
	return h
end

local function decodeBody(body)
	if type(body) ~= "string" or #body == 0 then
		return nil
	end
	local ok, decoded = LrTasks.pcall(function()
		return json.decode(body)
	end)
	if ok and type(decoded) == "table" then
		return decoded
	end
	return nil
end

local function parseResponse(body, headers, what)
	if headers == nil then
		return false, what .. " failed: no response from server (check the URL and your network)", nil, nil
	end
	local status = tonumber(headers.status) or 0
	local data = decodeBody(body)
	if status >= 200 and status < 300 then
		return true, data or {}, status, data
	end
	local msg = string.format("%s failed (HTTP %d)", what, status)
	if status == 401 then
		msg = msg .. ": API key rejected"
	elseif data and data.error then
		msg = msg .. ": " .. tostring(data.error)
		if data.message and data.message ~= "" then
			msg = msg .. " - " .. tostring(data.message)
		end
	end
	return false, msg, status, data
end

-- True for failures a server restart or redeploy explains, so retrying makes
-- sense. The backend's own 404s always carry an error code in the body; a
-- bare 404 comes from the reverse proxy while the container is down.
local function isTransient(status, data)
	if status == nil or status == 0 or status >= 500 then
		return true
	end
	return status == 404 and (data == nil or data.error == nil)
end

-- Calls send (returns body, headers) and retries transient failures. Stops
-- early if self.isCanceled is set and returns true.
local function sendWithRetry(self, send, what)
	local attempt = 1
	while true do
		local body, headers = send()
		local ok, result, status, data = parseResponse(body, headers, what)
		local delay = RETRY_DELAYS[attempt]
		if ok or delay == nil or not isTransient(status, data) then
			return ok, result, status, data
		end
		if self.isCanceled and self.isCanceled() then
			return ok, result, status, data
		end
		log:warnf("%s -> %s, retrying in %d s", what, tostring(result), delay)
		LrTasks.sleep(delay)
		attempt = attempt + 1
	end
end

-- JSON request. method is GET, POST, PUT or DELETE.
function SmugboxAPI:request(method, path, bodyTable, what)
	what = what or (method .. " " .. path)
	if not self:isConfigured() then
		return false, "Server URL or API key is not set (Publishing Manager > Smugbox)", nil, nil
	end
	local url = self.baseUrl .. path
	log:tracef("%s %s", method, url)
	local send
	if method == "GET" then
		send = function()
			return LrHttp.get(url, self:headers(false), TIMEOUT)
		end
	else
		local body = json.encode(bodyTable or setmetatable({}, { __jsontype = "object" }))
		send = function()
			return LrHttp.post(url, body, self:headers(true), method, TIMEOUT)
		end
	end
	local ok, result, status, data = sendWithRetry(self, send, what)
	if not ok then
		log:warnf("%s %s -> %s", method, url, tostring(result))
	end
	return ok, result, status, data
end

function SmugboxAPI:ping()
	return self:request("GET", "/api/publish/ping", nil, "Connection test")
end

function SmugboxAPI:createAlbum(fields)
	return self:request("POST", "/api/publish/albums", fields, "Create album")
end

function SmugboxAPI:updateAlbum(albumId, fields)
	return self:request("PUT", "/api/publish/albums/" .. albumId, fields, "Update album")
end

function SmugboxAPI:deleteAlbum(albumId)
	return self:request("DELETE", "/api/publish/albums/" .. albumId, nil, "Delete album")
end

function SmugboxAPI:listPhotos(albumId)
	return self:request("GET", "/api/publish/albums/" .. albumId .. "/photos", nil, "List photos")
end

function SmugboxAPI:deletePhoto(albumId, photoId)
	return self:request("DELETE", "/api/publish/albums/" .. albumId .. "/photos/" .. photoId, nil, "Delete photo")
end

function SmugboxAPI:setOrder(albumId, photoIds)
	return self:request("PUT", "/api/publish/albums/" .. albumId .. "/order", { photo_ids = photoIds }, "Set photo order")
end

function SmugboxAPI:createFolder(fields)
	return self:request("POST", "/api/publish/folders", fields, "Create album set")
end

function SmugboxAPI:updateFolder(folderId, fields)
	return self:request("PUT", "/api/publish/folders/" .. folderId, fields, "Update album set")
end

function SmugboxAPI:deleteFolder(folderId)
	return self:request("DELETE", "/api/publish/folders/" .. folderId, nil, "Delete album set")
end

-- Uploads filePath with metadata fields (all strings). With photoId the
-- call replaces that photo (the backend accepts POST on the photo path
-- because LrHttp.postMultipart cannot send PUT). Returns ok, result, status.
function SmugboxAPI:uploadPhoto(albumId, filePath, fields, photoId)
	if not self:isConfigured() then
		return false, "Server URL or API key is not set", nil, nil
	end
	local path = "/api/publish/albums/" .. albumId .. "/photos"
	if photoId then
		path = path .. "/" .. photoId
	end
	local url = self.baseUrl .. path
	local chunks = {}
	for name, value in pairs(fields) do
		if value ~= nil then
			table.insert(chunks, { name = name, value = tostring(value) })
		end
	end
	table.insert(chunks, {
		name = "file",
		filePath = filePath,
		fileName = fields.filename or LrPathUtils.leafName(filePath),
		contentType = "image/jpeg",
	})
	log:tracef("POST %s (multipart, %s)", url, tostring(fields.filename))
	local ok, result, status, data = sendWithRetry(self, function()
		return LrHttp.postMultipart(url, chunks, self:headers(false), UPLOAD_TIMEOUT)
	end, photoId and "Replace photo" or "Upload photo")
	if not ok then
		log:warnf("upload %s -> %s", url, tostring(result))
	end
	return ok, result, status, data
end

return SmugboxAPI
