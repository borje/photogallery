-- Self-check for the album-recreation recovery: when the server has lost
-- the album, every already-published photo is flagged for re-publish.
-- Run: lua test_republish.lua
local stub = require "test_stub"
local PublishTask = require "PublishTask"

local writes = 0
stub.catalog({ withWriteAccessDo = function(_, _, fn) writes = writes + 1; fn() end })

local function publishedPhoto()
	local pp = { edited = nil }
	function pp:setEditedFlag(v) self.edited = v end
	return pp
end

local photos = { publishedPhoto(), publishedPhoto(), publishedPhoto() }
local collection = {
	type = function() return "LrPublishedCollection" end,
	getPublishedPhotos = function() return photos end,
}

local n = PublishTask.markAllForRepublish(collection)
assert(n == 3, "should flag all three: " .. n)
for i, pp in ipairs(photos) do
	assert(pp.edited == true, "photo " .. i .. " not flagged")
end
assert(writes == 1, "all flags inside one write gate: " .. writes)

-- A failing catalog call is logged, not raised.
n = PublishTask.markAllForRepublish({
	type = function() return "LrPublishedCollection" end,
	getPublishedPhotos = function() error("catalog busy") end,
})
assert(n == 0, "failure should yield 0")

-- Nil or a collection set is a no-op.
assert(PublishTask.markAllForRepublish(nil) == 0)
assert(PublishTask.markAllForRepublish({ type = function() return "LrPublishedCollectionSet" end }) == 0)

-- processRenderedPhotos with nothing to render and a vanished album:
-- the album is recreated, photos are flagged, and the user is told.
stub.sdk.LrUUID.generateUUID = function() return "k" end
stub.reset()
stub.http(function(method, url)
	if method == "PUT" and url:match("/albums/old$") then
		return 404, '{"error":"album_not_found"}'
	elseif method == "POST" and url:match("/api/publish/albums$") then
		return 201, '{"id":"new","url":"https://example.test/a/new"}'
	end
	return 500, ""
end)
for _, pp in ipairs(photos) do pp.edited = nil end
local recorded = {}
local exportContext = {
	propertyTable = { serverUrl = "https://example.test", apiKey = "key" },
	publishedCollectionInfo = { remoteId = "old", name = "Iceland" },
	publishedCollection = {
		type = function() return "LrPublishedCollection" end,
		getParent = function() return nil end,
		getName = function() return "Iceland" end,
		getCollectionInfoSummary = function() return { collectionSettings = {} } end,
		getPublishedPhotos = function() return photos end,
	},
	exportSession = {
		countRenditions = function() return 0 end,
		recordRemoteCollectionId = function(_, id) recorded.id = id end,
		recordRemoteCollectionUrl = function(_, url) recorded.url = url end,
	},
	renditions = function() return function() return nil end end,
}
PublishTask.processRenderedPhotos({}, exportContext)
assert(recorded.id == "new" and recorded.url == "https://example.test/a/new", "new album should be recorded")
for i, pp in ipairs(photos) do
	assert(pp.edited == true, "photo " .. i .. " not flagged after recreate")
end
assert(#stub.dialogs == 1 and stub.dialogs[1].kind == "info", "user should be told once")
assert(stub.dialogs[1].message:match("3 previously published photos were marked"), stub.dialogs[1].message)

print("ok")
