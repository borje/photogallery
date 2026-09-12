-- Self-check: album and album-set creates carry one idempotency key across
-- all retries of a call, and a fresh key per call. Run: lua test_idempotency.lua
local stub = require "test_stub"
local json = require "dkjson"
local SmugboxAPI = require "SmugboxAPI"

local n = 0
stub.sdk.LrUUID.generateUUID = function()
	n = n + 1
	return "uuid-" .. n
end

local function keysSent()
	local keys = {}
	for _, c in ipairs(stub.calls) do
		table.insert(keys, json.decode(c.body).idempotency_key)
	end
	return keys
end

-- 502 then 201: both attempts must carry the same key.
local attempt = 0
stub.http(function(method, url)
	attempt = attempt + 1
	if attempt == 1 then
		return 502, ""
	end
	return 201, '{"id":"a1","url":"u"}'
end)
local api = SmugboxAPI.new("https://example.test", "key")
local ok, album = api:createAlbum({ name = "Iceland" })
assert(ok and album.id == "a1", tostring(album))
local keys = keysSent()
assert(#keys == 2 and keys[1] == "uuid-1" and keys[2] == "uuid-1", "retry must reuse the key: " .. table.concat(keys, ","))
assert(stub.calls[1].url:match("/api/publish/albums$"), stub.calls[1].url)

-- A second create is a new logical request with a new key.
stub.http(function() return 201, '{"id":"a2"}' end)
api:createAlbum({ name = "Iceland" })
assert(keysSent()[1] == "uuid-2", "second create should mint a new key")

-- Sets get one too, and a caller-supplied key is kept.
stub.http(function() return 201, '{"id":"f1"}' end)
api:createFolder({ name = "Travel", parent_id = "" })
assert(keysSent()[1] == "uuid-3", "folder create should carry a key")
stub.http(function() return 201, '{"id":"f2"}' end)
api:createFolder({ name = "Travel", idempotency_key = "mine" })
assert(keysSent()[1] == "mine", "explicit key should be kept")

-- Other calls are untouched.
stub.http(function() return 200, '{"id":"a1"}' end)
api:updateAlbum("a1", { name = "X" })
assert(json.decode(stub.calls[1].body).idempotency_key == nil, "update must not carry a key")

-- The key outlives the call: a create whose whole retry ladder failed must
-- present the same key when the user publishes again, or a create that did
-- reach the backend turns into a duplicate album.
local PublishTask = require "PublishTask"
local catalog = stub.catalog()
local collection = { localIdentifier = 42 }

local first = PublishTask.pendingCreateKey(collection)
assert(type(first) == "string" and first ~= "", "first key: " .. tostring(first))
assert(catalog.properties["createKey.42"] == first, "key not persisted")
assert(PublishTask.pendingCreateKey(collection) == first, "a later publish must reuse the stored key")

-- Recording the create forgets it, so the next create is a new one.
PublishTask.clearPendingCreateKey(collection)
assert(catalog.properties["createKey.42"] == nil, "key not cleared")
assert(PublishTask.pendingCreateKey(collection) ~= first, "a new create must mint a new key")

-- A collection without a stable identity still gets a per-call key.
assert(PublishTask.pendingCreateKey({}) ~= nil, "fallback key")

-- Album sets go through the same store, via resolveParent.
stub.reset()
catalog.properties = {}
local set = {
	localIdentifier = 7,
	getRemoteId = function() return nil end,
	getName = function() return "Travel" end,
	getParent = function() return nil end,
	setRemoteId = function() end,
	setRemoteUrl = function() end,
}
local child = { getParent = function() return set end }
stub.http(function() return nil, nil end) -- every attempt fails outright
local ok = PublishTask.resolveParent({
	createFolder = function(_, fields)
		return false, "boom", nil, nil, fields
	end,
}, child)
assert(not ok, "create failure should be reported")
local stored = catalog.properties["createKey.7"]
assert(type(stored) == "string" and stored ~= "", "set key not persisted: " .. tostring(stored))

local sent
local ok2, parentId = PublishTask.resolveParent({
	createFolder = function(_, fields)
		sent = fields
		return true, { id = "f9", url = "u" }
	end,
}, child)
assert(ok2 and parentId == "f9", "second attempt should create the folder")
assert(sent.idempotency_key == stored, "the retry must reuse the stored key")
assert(catalog.properties["createKey.7"] == nil, "set key not cleared after the create")

print("ok")
