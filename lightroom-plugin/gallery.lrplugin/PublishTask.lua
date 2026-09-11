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
local LrTasks = import "LrTasks"
local LrView = import "LrView"

local json = require "dkjson"
local GalleryAPI = require "GalleryAPI"
local Util = require "Util"

local PublishTask = {}

-- Album fields sent to the backend, from the collection name, the
-- per-collection settings (password, listed, description) and the backend
-- folder id of the containing album set ("" for the root).
function PublishTask.albumFields(name, collectionSettings, parentId)
	collectionSettings = collectionSettings or {}
	local fields = { name = name }
	fields.password = collectionSettings.password or ""
	if collectionSettings.isListed == nil then
		fields.is_listed = true
	else
		fields.is_listed = collectionSettings.isListed and true or false
	end
	fields.description = collectionSettings.description or ""
	fields.parent_id = parentId or ""
	return fields
end

-- Walks the chain of published collection sets containing `collection`,
-- root first, creating a backend folder for any set that doesn't have one
-- yet and recording its id on the set. Returns ok, and either the innermost
-- folder id ("" at the root) or an error message.
function PublishTask.resolveParent(api, collection)
	if not collection then
		return true, nil
	end
	local chain = {}
	local set = collection:getParent()
	while set do
		table.insert(chain, 1, set) -- root-first
		set = set:getParent()
	end
	local parentId = nil
	for _, s in ipairs(chain) do
		local id = s:getRemoteId()
		if not id then
			local ok, folder = api:createFolder({ name = s:getName(), parent_id = parentId })
			if not ok then
				return false, folder
			end
			id = folder.id
			LrApplication.activeCatalog():withWriteAccessDo("Photo Gallery: store album set id", function()
				s:setRemoteId(id)
				s:setRemoteUrl(folder.url)
			end)
			log:infof("created album set %s (%s)", id, folder.url)
		end
		parentId = id
	end
	return true, parentId
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

-- Cover photo. The collection settings store the catalog uuid of the chosen
-- photo (coverPhotoUuid, "" = let the server use the first photo in sort
-- order), because the photo may not have been published yet when it is
-- picked. It is translated to the server's photo id whenever settings are
-- saved and after every publish run.
--
-- Returns the value for cover_photo_id: "" when unset, the remote id when the
-- photo is published, or nil when it cannot be resolved yet. `uploaded` maps
-- catalog uuid -> remote id for photos uploaded in the current run, which
-- the collection's published-photo list does not reflect until it ends.
function PublishTask.resolveCoverId(collection, coverUuid, uploaded)
	if coverUuid == nil or coverUuid == "" then
		return ""
	end
	if uploaded and uploaded[coverUuid] then
		return uploaded[coverUuid]
	end
	if not collection or collection:type() ~= "LrPublishedCollection" then
		return nil
	end
	local ok, remoteId = LrTasks.pcall(function()
		for _, pp in ipairs(collection:getPublishedPhotos()) do
			if pp:getPhoto():getRawMetadata("uuid") == coverUuid then
				return pp:getRemoteId()
			end
		end
		return nil
	end)
	if not ok then
		log:warnf("resolve cover: %s", tostring(remoteId))
		return nil
	end
	return remoteId
end

-- Sends the cover to the server if it can be resolved; failures are logged,
-- never fatal, since the photos themselves are already published.
function PublishTask.pushCover(api, albumId, collection, collectionSettings, uploaded)
	local coverId = PublishTask.resolveCoverId(collection, (collectionSettings or {}).coverPhotoUuid, uploaded)
	if coverId == nil then
		log:infof("cover photo not published yet, leaving server cover unchanged")
		return
	end
	local ok, result = api:updateAlbum(albumId, { cover_photo_id = coverId })
	if not ok then
		log:warnf("set cover: %s", tostring(result))
	end
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

	local parentOk, parentId = PublishTask.resolveParent(api, publishedCollection)
	if not parentOk then
		progress:done()
		stopWithError("Photo Gallery: " .. tostring(parentId))
	end

	local function createAlbum()
		local ok, album = api:createAlbum(PublishTask.albumFields(albumName, collectionSettings, parentId))
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
	else
		-- Reconcile in case the collection was dragged into a different set
		-- since the last publish; Lightroom fires no callback for that.
		local ok, result = api:updateAlbum(albumId, { parent_id = parentId or "" })
		if not ok then
			log:warnf("update album set: %s", tostring(result))
		end
	end

	local failures = {}
	local uploaded = {} -- catalog uuid -> remote id, for the cover lookup
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
				uploaded[fields.lr_photo_uuid] = result.id
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
	PublishTask.pushCover(api, albumId, publishedCollection, collectionSettings, uploaded)

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


-- Per-collection settings dialog. Values persist with the collection in the
-- catalog (in clear text, which is acceptable for album passwords).
-- Photos of the collection being edited, in its sort order; empty for a
-- collection that is still being created or when the catalog cannot be read.
local function collectionPhotos(collection)
	if not collection or collection:type() ~= "LrPublishedCollection" then
		return {}
	end
	local ok, photos = LrTasks.pcall(function()
		return collection:getPhotos()
	end)
	if not ok then
		log:warnf("collection photos: %s", tostring(photos))
		return {}
	end
	return photos or {}
end

-- Popup menu + live thumbnail for the cover photo. Lightroom's view kit has
-- no clickable thumbnail grid, so the photo is chosen by file name and the
-- catalog_photo view next to it shows what was picked.
local function coverPicker(f, settings, info)
	local bind = LrView.bind
	local share = LrView.share
	local photos = collectionPhotos(info.publishedCollection)
	if #photos == 0 then
		local hint = info.publishedCollection
				and "The album has no photos yet."
			or "Create the album and publish it once, then edit its settings to pick a cover photo."
		return f:row {
			f:static_text { title = "Cover photo:", alignment = "right", width = share "albumLabel" },
			f:static_text { title = hint, font = "<system/small>" },
		}
	end

	local items = { { title = "First photo in the album", value = "" } }
	local byUuid = {}
	for _, photo in ipairs(photos) do
		local uuid = photo:getRawMetadata("uuid")
		local label = photo:getFormattedMetadata("fileName") or uuid
		local title = photo:getFormattedMetadata("title")
		if title and title ~= "" then
			label = label .. " – " .. title
		end
		byUuid[uuid] = photo
		table.insert(items, { title = label, value = uuid })
	end
	if not byUuid[settings.coverPhotoUuid] then
		settings.coverPhotoUuid = "" -- picked photo has left the collection
	end

	return f:row {
		f:static_text { title = "Cover photo:", alignment = "right", width = share "albumLabel" },
		f:column {
			spacing = f:control_spacing(),
			fill_horizontal = 1,
			f:popup_menu { value = bind "coverPhotoUuid", items = items, fill_horizontal = 1 },
			f:catalog_photo {
				photo = bind {
					key = "coverPhotoUuid",
					transform = function(value)
						return byUuid[value] or photos[1]
					end,
				},
				width = 240,
				height = 160,
			},
			f:static_text {
				title = "Shown in the album list and at the top of the album page. A photo that is not published yet becomes the cover on the next publish.",
				font = "<system/small>",
			},
		},
	}
end

function PublishTask.viewForCollectionSettings(f, publishSettings, info)
	local settings = assert(info.collectionSettings)
	if settings.isListed == nil then
		settings.isListed = true
	end
	if settings.password == nil then
		settings.password = ""
	end
	if settings.description == nil then
		settings.description = ""
	end
	if settings.coverPhotoUuid == nil then
		settings.coverPhotoUuid = ""
	end
	local bind = LrView.bind
	local share = LrView.share
	return f:group_box {
		title = "Album settings",
		fill_horizontal = 1,
		bind_to_object = settings,
		f:row {
			f:static_text { title = "Password:", alignment = "right", width = share "albumLabel" },
			f:password_field { value = bind "password", immediate = true, fill_horizontal = 1 },
		},
		f:row {
			f:static_text { title = "", width = share "albumLabel" },
			f:static_text {
				title = "Leave empty for a public album. Visitors must enter the password to view and download.",
				font = "<system/small>",
			},
		},
		f:row {
			f:static_text { title = "", width = share "albumLabel" },
			f:checkbox { title = "Show in the public album list", value = bind "isListed" },
		},
		f:row {
			f:static_text { title = "Description:", alignment = "right", width = share "albumLabel" },
			f:edit_field { value = bind "description", immediate = true, fill_horizontal = 1, height_in_lines = 3 },
		},
		coverPicker(f, settings, info),
	}
end

-- Called after the collection settings dialog is confirmed. Sends name,
-- password, listing, description and cover; the backend only bumps the
-- password version when the password actually changed.
--
-- Not published yet (no remoteId): do nothing here. Lightroom discards any
-- remoteId we record from within this callback for a collection that has
-- never been published, so creating the album here would just leave it
-- orphaned and repeat on every settings save. The first publish creates the
-- album with these collectionSettings already applied.
--
-- Read the remoteId straight from the collection object rather than
-- info.remoteId: in practice info.remoteId is not reliably populated here,
-- even for collections that have already been published.
function PublishTask.updateCollectionSettings(publishSettings, info)
	local remoteId = info.publishedCollection and info.publishedCollection:getRemoteId()
	if not remoteId then
		return
	end
	local api = GalleryAPI.new(publishSettings.serverUrl, publishSettings.apiKey)
	if not api:isConfigured() then
		return
	end
	local parentOk, parentId = PublishTask.resolveParent(api, info.publishedCollection)
	if not parentOk then
		LrDialogs.message("Photo Gallery: could not resolve album set on server", tostring(parentId), "warning")
		return
	end
	local fields = PublishTask.albumFields(info.name, info.collectionSettings, parentId)
	fields.cover_photo_id = PublishTask.resolveCoverId(info.publishedCollection, (info.collectionSettings or {}).coverPhotoUuid)
	local ok, result = api:updateAlbum(remoteId, fields)
	if not ok then
		LrDialogs.message("Photo Gallery: album settings not saved on server", tostring(result), "warning")
	end
end

-- Lightroom calls rename/delete for both published collections and
-- published collection sets; branch on which one this is.
local function isCollectionSet(info)
	return info.publishedCollection ~= nil and info.publishedCollection:type() == "LrPublishedCollectionSet"
end

function PublishTask.renamePublishedCollection(publishSettings, info)
	if not info.remoteId then
		return
	end
	local api = GalleryAPI.new(publishSettings.serverUrl, publishSettings.apiKey)
	local ok, result
	if isCollectionSet(info) then
		ok, result = api:updateFolder(info.remoteId, { name = info.name })
	else
		ok, result = api:updateAlbum(info.remoteId, { name = info.name })
	end
	if not ok then
		LrDialogs.message("Photo Gallery: not renamed on server", tostring(result), "warning")
	end
end

function PublishTask.deletePublishedCollection(publishSettings, info)
	if not info.remoteId then
		return
	end
	local api = GalleryAPI.new(publishSettings.serverUrl, publishSettings.apiKey)
	local ok, result, status
	if isCollectionSet(info) then
		ok, result, status = api:deleteFolder(info.remoteId)
	else
		ok, result, status = api:deleteAlbum(info.remoteId)
	end
	if not ok and status ~= 404 then
		LrDialogs.message("Photo Gallery: not deleted on server", tostring(result), "warning")
	end
end

function PublishTask.deletePhotosFromPublishedCollection(publishSettings, arrayOfPhotoIds, deletedCallback, localCollectionId)
	local api = GalleryAPI.new(publishSettings.serverUrl, publishSettings.apiKey)
	local collection = LrApplication.activeCatalog():getPublishedCollectionByLocalIdentifier(localCollectionId)
	local albumId = collection and collection:getRemoteId() or nil
	if not albumId then
		-- Nothing exists on the server; let Lightroom forget the photos.
		for _, id in ipairs(arrayOfPhotoIds) do
			deletedCallback(id)
		end
		return
	end
	local failed = 0
	for _, id in ipairs(arrayOfPhotoIds) do
		local ok, result, status = api:deletePhoto(albumId, id)
		if ok or status == 404 then
			deletedCallback(id)
		else
			failed = failed + 1
			log:warnf("delete photo %s: %s", id, tostring(result))
		end
	end
	if failed > 0 then
		LrDialogs.message("Photo Gallery: some photos were not deleted on the server", string.format("%d photo(s) failed. See the log for details.", failed), "warning")
	end
end

function PublishTask.imposeSortOrderOnPublishedCollection(publishSettings, info, remoteIdSequence)
	if not info.remoteId then
		return false
	end
	local api = GalleryAPI.new(publishSettings.serverUrl, publishSettings.apiKey)
	local ok, result = api:setOrder(info.remoteId, remoteIdSequence)
	if not ok then
		log:warnf("set order: %s", tostring(result))
	end
	return ok
end

return PublishTask
