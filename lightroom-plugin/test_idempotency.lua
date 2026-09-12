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

print("ok")
