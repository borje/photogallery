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

-- The album can also vanish part way through a run. Photos already
-- uploaded went into the album that is now gone, and Lightroom clears their
-- edited flag at the end of the run, so markAllForRepublish does not bring
-- them back: the dialog has to name them.
stub.reset()
local function photoStub(uuid)
	return {
		getRawMetadata = function(_, key) return key == "uuid" and uuid or nil end,
		getFormattedMetadata = function() return "" end,
	}
end
local function renditionStub(uuid, path)
	local r = { photo = photoStub(uuid), publishedPhotoId = nil }
	function r:waitForRender() return true, path end
	function r:recordPublishedPhotoId(id) self.recordedId = id end
	function r:recordPublishedPhotoUrl(url) self.recordedUrl = url end
	function r:uploadFailed(msg) self.failed = msg end
	return r
end
local first = renditionStub("u1", "first.jpg")
local second = renditionStub("u2", "second.jpg")
local uploads = 0
stub.http(function(method, url)
	if method == "PUT" then
		return 200, '{"id":"old"}'
	elseif method == "POST" and url:match("/api/publish/albums$") then
		return 201, '{"id":"new","url":"https://example.test/a/new"}'
	elseif method == "POST" and url:match("/photos$") then
		uploads = uploads + 1
		if uploads == 2 then
			return 404, '{"error":"album_not_found"}'
		end
		return 201, string.format('{"id":"p%d"}', uploads)
	end
	return 500, ""
end)
for _, pp in ipairs(photos) do pp.edited = nil end
exportContext.exportSession.countRenditions = function() return 2 end
exportContext.renditions = function()
	local list, i = { first, second }, 0
	return function()
		i = i + 1
		if list[i] then
			return i, list[i]
		end
	end
end
PublishTask.processRenderedPhotos({}, exportContext)
assert(first.recordedId == "p1", "first photo should have been published into the old album")
assert(second.recordedId == "p3", "second photo should have been published into the new album: " .. tostring(second.recordedId))
local msg = stub.dialogs[1] and stub.dialogs[1].message or ""
assert(msg:match("first%.jpg"), "the dialog must name the photo left in the deleted album: " .. msg)
assert(not msg:match("second%.jpg"), "the photo that reached the new album must not be listed: " .. msg)
assert(msg:match("Mark to Re%-publish"), "the dialog must say what to do: " .. msg)

print("ok")
