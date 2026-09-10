--[[
Implementation of the publish service callbacks. The provider table in
PublishServiceProvider.lua points at these functions.
]]

local LrApplication = import "LrApplication"
local LrDate = import "LrDate"
local LrDialogs = import "LrDialogs"
local LrErrors = import "LrErrors"
local LrPathUtils = import "LrPathUtils"
local LrProgressScope = import "LrProgressScope"

local json = require "dkjson"
local GalleryAPI = require "GalleryAPI"
local Util = require "Util"

local PublishTask = {}

-- Album fields sent to the backend, from the collection name and the
-- per-collection settings (password, listed, description).
function PublishTask.albumFields(name, collectionSettings)
	collectionSettings = collectionSettings or {}
	local fields = { name = name }
	fields.password = collectionSettings.password or ""
	if collectionSettings.isListed == nil then
		fields.is_listed = true
	else
		fields.is_listed = collectionSettings.isListed and true or false
	end
	fields.description = collectionSettings.description or ""
	return fields
end

local exifKeys = {
	{ "cameraMake", "make" },
	{ "cameraModel", "model" },
	{ "lens", "lens" },
	{ "exposure", "exposure" },
	{ "focalLength", "focal_length" },
	{ "isoSpeedRating", "iso" },
	{ "aperture", "aperture" },
	{ "shutterSpeed", "shutter_speed" },
}

-- Form fields describing one photo. Everything is read from the catalog,
-- not from the exported file, so it survives "minimize embedded metadata".
function PublishTask.metadataFields(photo, renderedPath)
	local fields = {}
	fields.lr_photo_uuid = photo:getRawMetadata("uuid")
	fields.filename = LrPathUtils.leafName(renderedPath)
	fields.title = photo:getFormattedMetadata("title") or ""
	fields.caption = photo:getFormattedMetadata("caption") or ""

	local keywords = {}
	local kw = photo:getFormattedMetadata("keywordTagsForExport") or ""
	for k in string.gmatch(kw, "[^,]+") do
		k = Util.trim(k)
		if k ~= "" then
			table.insert(keywords, k)
		end
	end
	fields.keywords = #keywords > 0 and json.encode(keywords) or "[]"

	local takenAt = photo:getRawMetadata("dateTimeOriginalISO8601")
	if takenAt == nil or takenAt == "" then
		local t = photo:getRawMetadata("dateTimeOriginal")
		if t then
			takenAt = LrDate.timeToUserFormat(t, "%Y-%m-%dT%H:%M:%S")
		end
	end
	fields.taken_at = takenAt or ""

	local exif = setmetatable({}, { __jsontype = "object" })
	for _, pair in ipairs(exifKeys) do
		local v = photo:getFormattedMetadata(pair[1])
		if v ~= nil and v ~= "" then
			exif[pair[2]] = tostring(v)
		end
	end
	fields.exif = json.encode(exif)
	return fields
end

local function stopWithError(message)
	log:error(message)
	LrErrors.throwUserError(message)
end

-- Publishes every rendition in the export session.
function PublishTask.processRenderedPhotos(functionContext, exportContext)
	local exportSession = exportContext.exportSession
	local settings = exportContext.propertyTable
	local api = GalleryAPI.new(settings.serverUrl, settings.apiKey)
	if not api:isConfigured() then
		stopWithError("Photo Gallery: set the server URL and API key in the Publishing Manager first.")
	end

	local collectionInfo = exportContext.publishedCollectionInfo
	local publishedCollection = exportContext.publishedCollection
	local albumId = collectionInfo and collectionInfo.remoteId or nil
	local collectionSettings = {}
	if publishedCollection then
		local ok, summary = pcall(function()
			return publishedCollection:getCollectionInfoSummary()
		end)
		if ok and summary and summary.collectionSettings then
			collectionSettings = summary.collectionSettings
		end
	end
	local albumName = (collectionInfo and collectionInfo.name) or (publishedCollection and publishedCollection:getName()) or "Album"

	local nPhotos = exportSession:countRenditions()
	local progress = LrProgressScope({
		title = string.format("Publishing %d photo%s to Photo Gallery", nPhotos, nPhotos == 1 and "" or "s"),
		functionContext = functionContext,
	})

	local function createAlbum()
		local ok, album = api:createAlbum(PublishTask.albumFields(albumName, collectionSettings))
		if not ok then
			progress:done()
			stopWithError("Photo Gallery: " .. tostring(album))
		end
		log:infof("created album %s (%s)", album.id, album.url)
		exportSession:recordRemoteCollectionId(album.id)
		exportSession:recordRemoteCollectionUrl(album.url)
		return album.id
	end

	if not albumId then
		albumId = createAlbum()
	end

	local failures = {}
	local albumRecreated = false
	for i, rendition in exportContext:renditions({ stopIfCanceled = true }) do
		progress:setPortionComplete(i - 1, nPhotos)
		local success, pathOrMessage = rendition:waitForRender()
		if progress:isCanceled() then
			break
		end
		if success then
			local photo = rendition.photo
			local fields = PublishTask.metadataFields(photo, pathOrMessage)
			local existingId = rendition.publishedPhotoId
			local ok, result, status, body

			if existingId then
				ok, result, status, body = api:uploadPhoto(albumId, pathOrMessage, fields, existingId)
				if not ok and status == 404 and body and body.error == "photo_not_found" then
					log:infof("photo %s gone on server, uploading as new", existingId)
					ok, result, status, body = api:uploadPhoto(albumId, pathOrMessage, fields, nil)
				end
			else
				ok, result, status, body = api:uploadPhoto(albumId, pathOrMessage, fields, nil)
			end

			-- The album itself may have been removed server-side (CLI, gc).
			if not ok and status == 404 and body and body.error == "album_not_found" and not albumRecreated then
				log:warnf("album %s gone on server, creating a new one", albumId)
				albumRecreated = true
				albumId = createAlbum()
				ok, result, status, body = api:uploadPhoto(albumId, pathOrMessage, fields, nil)
			end

			if ok and type(result) == "table" and result.id then
				rendition:recordPublishedPhotoId(result.id)
				if result.url then
					rendition:recordPublishedPhotoUrl(result.url)
				end
				log:tracef("published %s as %s", fields.filename, result.id)
			else
				local msg = tostring(result)
				rendition:uploadFailed(msg)
				table.insert(failures, fields.filename .. ": " .. msg)
			end
			Util.safeDelete(pathOrMessage)
		else
			table.insert(failures, tostring(pathOrMessage))
		end
	end
	progress:done()

	if #failures > 0 then
		local shown = {}
		for i = 1, math.min(#failures, 10) do
			shown[i] = failures[i]
		end
		if #failures > 10 then
			table.insert(shown, string.format("... and %d more", #failures - 10))
		end
		LrDialogs.message(
			string.format("%d photo%s could not be published", #failures, #failures == 1 and "" or "s"),
			table.concat(shown, "\n"),
			"warning"
		)
	end
end

return PublishTask
