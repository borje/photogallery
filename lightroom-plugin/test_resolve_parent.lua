-- Self-check for PublishTask.resolveParent repair mode: a collection set
-- whose folder was deleted server-side gets a new folder instead of every
-- later create failing with folder_not_found. Run: lua test_resolve_parent.lua
local stub = require "test_stub"
local json = require "dkjson"
local PublishTask = require "PublishTask"
local SmugboxAPI = require "SmugboxAPI"

stub.sdk.LrUUID.generateUUID = function() return "k" end
stub.catalog()

-- Builds a set with a stored remote id (or nil) and records setRemoteId.
local function makeSet(name, remoteId, parent)
	local set = { stored = remoteId, name = name }
	function set:getRemoteId() return self.stored end
	function set:getName() return self.name end
	function set:getParent() return parent end
	function set:setRemoteId(id) self.stored = id; self.set = id end
	function set:setRemoteUrl(u) self.url = u end
	return set
end

local function collectionIn(set)
	return { getParent = function() return set end }
end

local api = SmugboxAPI.new("https://example.test", "key")

-- Stale id on the inner set, valid id on the outer one.
local outer = makeSet("Travel", "outer-ok")
local inner = makeSet("2024", "inner-stale", outer)
stub.http(function(method, url, body)
	if method == "PUT" and url:match("/folders/outer%-ok$") then
		return 200, '{"id":"outer-ok"}'
	elseif method == "PUT" and url:match("/folders/inner%-stale$") then
		return 404, '{"error":"folder_not_found"}'
	elseif method == "POST" and url:match("/api/publish/folders$") then
		return 201, '{"id":"inner-new","url":"https://example.test/f/2024"}'
	end
	return 500, ""
end)
local ok, parentId = PublishTask.resolveParent(api, collectionIn(inner), true)
assert(ok and parentId == "inner-new", tostring(parentId))
assert(inner.set == "inner-new" and inner.url == "https://example.test/f/2024", "stale set should get the new id")
assert(outer.set == nil, "valid set must keep its id")
assert(#stub.calls == 3, "two verifications and one create: " .. #stub.calls)
local created = json.decode(stub.calls[3].body)
assert(created.parent_id == "outer-ok" and created.name == "2024", stub.calls[3].body)

-- Without repair, stored ids are trusted and no request is made.
inner = makeSet("2024", "inner-stale", makeSet("Travel", "outer-ok"))
stub.http(function() return 500, "" end)
ok, parentId = PublishTask.resolveParent(api, collectionIn(inner))
assert(ok and parentId == "inner-stale" and #stub.calls == 0, "no repair, no requests")

-- Repair still fails loudly on anything other than folder_not_found.
stub.http(function() return 401, '{"error":"unauthorized"}' end)
ok, parentId = PublishTask.resolveParent(api, collectionIn(inner), true)
assert(not ok and tostring(parentId):match("401"), tostring(parentId))

-- A root-level collection resolves to nil without requests, repair or not.
stub.http(function() return 500, "" end)
ok, parentId = PublishTask.resolveParent(api, { getParent = function() return nil end }, true)
assert(ok and parentId == nil and #stub.calls == 0)

-- isFolderGone recognises the backend's 400 for a stale parent_id only.
assert(PublishTask.isFolderGone(false, 400, { error = "folder_not_found" }))
assert(not PublishTask.isFolderGone(false, 404, { error = "folder_not_found" }))
assert(not PublishTask.isFolderGone(false, 400, { error = "invalid_name" }))
assert(not PublishTask.isFolderGone(true, 201, {}))

-- updateCollectionSettings: a 400 folder_not_found repairs the chain and
-- saves on the second try.
local set = makeSet("Travel", "stale")
local puts = 0
stub.http(function(method, url, body)
	if method == "PUT" and url:match("/albums/a1$") then
		puts = puts + 1
		local parent = json.decode(body).parent_id
		if parent == "stale" then
			return 400, '{"error":"folder_not_found"}'
		end
		return 200, '{"id":"a1"}'
	elseif method == "PUT" and url:match("/folders/stale$") then
		return 404, '{"error":"folder_not_found"}'
	elseif method == "POST" and url:match("/folders$") then
		return 201, '{"id":"fresh","url":"u"}'
	end
	return 500, ""
end)
stub.reset()
PublishTask.updateCollectionSettings({ serverUrl = "https://example.test", apiKey = "key" }, {
	name = "Iceland",
	collectionSettings = {},
	publishedCollection = {
		getRemoteId = function() return "a1" end,
		getParent = function() return set end,
		type = function() return "LrPublishedCollection" end,
	},
})
assert(puts == 2, "album update should be retried once: " .. puts)
assert(set.set == "fresh", "set should have the recreated folder id")
assert(#stub.dialogs == 0, "no warning when the retry succeeds")

print("ok")
