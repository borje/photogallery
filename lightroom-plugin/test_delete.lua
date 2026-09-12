-- Self-check for delete handling in PublishTask: a bare 404 from the proxy
-- must not make Lightroom forget photos or collections the server still
-- has, while the backend's own *_not_found answers count as "already gone".
-- Run: lua test_delete.lua
local stub = require "test_stub"
local PublishTask = require "PublishTask"

local settings = { serverUrl = "https://example.test", apiKey = "key" }
local bare404 = function() return 404, "404 page not found" end

-- deletePhotosFromPublishedCollection ----------------------------------------

local function deletePhotos(handler)
	stub.reset()
	stub.http(handler)
	stub.catalog({ getPublishedCollectionByLocalIdentifier = function()
		return { getRemoteId = function() return "album1" end }
	end })
	local deleted = {}
	PublishTask.deletePhotosFromPublishedCollection(settings, { "p1", "p2" }, function(id)
		table.insert(deleted, id)
	end, 42)
	return deleted
end

local deleted = deletePhotos(bare404)
assert(#deleted == 0, "bare 404 must not forget photos: " .. #deleted)
assert(#stub.dialogs == 1, "bare 404 should warn once")
assert(#stub.calls == 12, "each photo retries the full ladder: " .. #stub.calls)

deleted = deletePhotos(function() return 404, '{"error":"photo_not_found"}' end)
assert(#deleted == 2, "photo_not_found means already gone: " .. #deleted)
assert(#stub.dialogs == 0, "no dialog for photo_not_found")

deleted = deletePhotos(function() return 404, '{"error":"album_not_found"}' end)
assert(#deleted == 2, "album_not_found means already gone: " .. #deleted)

deleted = deletePhotos(function() return 204, "" end)
assert(#deleted == 2 and #stub.calls == 2, "204 deletes without retry")

deleted = deletePhotos(function() return 401, '{"error":"unauthorized"}' end)
assert(#deleted == 0 and #stub.dialogs == 1, "401 keeps the photos and warns")

-- deletePublishedCollection --------------------------------------------------

local function deleteCollection(handler, isSet)
	stub.reset()
	stub.http(handler)
	local info = { remoteId = "r1", publishedCollection = { type = function()
		return isSet and "LrPublishedCollectionSet" or "LrPublishedCollection"
	end } }
	PublishTask.deletePublishedCollection(settings, info)
end

deleteCollection(bare404)
assert(#stub.dialogs == 1, "bare 404 on album delete should warn")
assert(stub.calls[1].method == "DELETE" and stub.calls[1].url:match("/api/publish/albums/r1$"), stub.calls[1].url)

deleteCollection(function() return 404, '{"error":"album_not_found"}' end)
assert(#stub.dialogs == 0, "album_not_found is silent")

deleteCollection(function() return 404, '{"error":"folder_not_found"}' end, true)
assert(#stub.dialogs == 0, "folder_not_found is silent")
assert(stub.calls[1].url:match("/api/publish/folders/r1$"), stub.calls[1].url)

deleteCollection(bare404, true)
assert(#stub.dialogs == 1, "bare 404 on set delete should warn")

deleteCollection(function() return 204, "" end)
assert(#stub.dialogs == 0 and #stub.calls == 1, "204 is silent")

-- isGone ----------------------------------------------------------------------

assert(not PublishTask.isGone(404, nil, "photo_not_found"))
assert(not PublishTask.isGone(404, {}, "photo_not_found"))
assert(not PublishTask.isGone(500, { error = "photo_not_found" }, "photo_not_found"))
assert(PublishTask.isGone(404, { error = "photo_not_found" }, "album_not_found", "photo_not_found"))

print("ok")
