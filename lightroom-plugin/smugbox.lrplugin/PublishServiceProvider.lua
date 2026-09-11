--[[
Publish service provider table for the Smugbox backend.

Structure follows Adobe's Flickr sample and lrc-immich-plugin: this file
holds the declarative parts and the settings dialog, PublishTask.lua the
callbacks that talk to the server.
]]

local LrDialogs = import "LrDialogs"
local LrHttp = import "LrHttp"
local LrTasks = import "LrTasks"
local LrView = import "LrView"

local SmugboxAPI = require "SmugboxAPI"
local PublishTask = require "PublishTask"

local bind = LrView.bind
local share = LrView.share

local provider = {}

-- Publish only; no plain export variant.
provider.supportsIncrementalPublish = "only"

-- Service-level settings persisted with the publish connection, plus
-- defaults for Lightroom's own export settings. The size section stays
-- visible: the backend generates display sizes from whatever is uploaded,
-- and the default is "do not resize".
provider.exportPresetFields = {
	{ key = "serverUrl", default = "" },
	{ key = "apiKey", default = "" },
	{ key = "LR_format", default = "JPEG" },
	{ key = "LR_export_colorSpace", default = "sRGB" },
	{ key = "LR_jpeg_quality", default = 0.9 },
	{ key = "LR_size_doConstrain", default = false },
	{ key = "LR_removeLocationMetadata", default = true },
}

provider.hideSections = { "exportLocation", "video", "postProcessing" }
provider.allowFileFormats = { "JPEG" }
provider.allowColorSpaces = { "sRGB" }
provider.canExportVideo = false

provider.titleForPublishedCollection = "Album"
provider.titleForPublishedCollectionSet = "Album set"
provider.titleForGoToPublishedCollection = "Open album in browser"
provider.titleForGoToPublishedPhoto = "Open photo in browser"

provider.canAddCommentsToService = false
provider.supportsCustomSortOrder = true

-- Changing these in the catalog marks the photo as "modified" for republish.
-- Develop edits always do.
function provider.metadataThatTriggersRepublish(publishSettings)
	return {
		default = false,
		title = true,
		caption = true,
		keywords = true,
		dateCreated = true,
	}
end

function provider.shouldDeletePhotosFromServiceOnDeleteFromCatalog(publishSettings, nPhotos)
	return "ask"
end

function provider.validatePublishedCollectionName(proposedName)
	if proposedName == nil or proposedName:match("^%s*$") then
		return false, "The album needs a name."
	end
	return true
end

function provider.startDialog(propertyTable)
	if propertyTable.connectionStatus == nil then
		propertyTable.connectionStatus = ""
	end
end

function provider.sectionsForTopOfDialog(f, propertyTable)
	return {
		{
			title = "Smugbox server",
			synopsis = bind { key = "serverUrl", object = propertyTable },
			bind_to_object = propertyTable,

			f:row {
				f:static_text { title = "Server URL:", alignment = "right", width = share "labelWidth" },
				f:edit_field {
					value = bind "serverUrl",
					immediate = true,
					fill_horizontal = 1,
					tooltip = "For example https://photos.example.com",
				},
			},
			f:row {
				f:static_text { title = "API key:", alignment = "right", width = share "labelWidth" },
				f:password_field {
					value = bind "apiKey",
					immediate = true,
					fill_horizontal = 1,
					tooltip = "Create one on the server with: smugbox admin create-api-key",
				},
			},
			f:row {
				f:static_text { title = "", width = share "labelWidth" },
				f:push_button {
					title = "Test connection",
					action = function()
						propertyTable.connectionStatus = "Testing..."
						LrTasks.startAsyncTask(function()
							local api = SmugboxAPI.new(propertyTable.serverUrl, propertyTable.apiKey)
							local ok, result = api:ping()
							if ok then
								propertyTable.connectionStatus = "Connected"
							else
								propertyTable.connectionStatus = "Failed"
								LrDialogs.message("Connection failed", tostring(result), "critical")
							end
						end)
					end,
				},
				f:static_text { title = bind "connectionStatus", fill_horizontal = 1 },
			},
		},
	}
end

function provider.getCollectionBehaviorInfo(publishSettings)
	return {
		defaultCollectionName = "Album",
		defaultCollectionCanBeDeleted = true,
		canAddCollection = true,
		maxCollectionSetDepth = 10, -- album sets nest arbitrarily deep on the server; this just bounds the UI
	}
end

function provider.goToPublishedCollection(publishSettings, info)
	if info.remoteUrl then
		LrHttp.openUrlInBrowser(info.remoteUrl)
	end
end

function provider.goToPublishedPhoto(publishSettings, info)
	if info.remoteUrl then
		LrHttp.openUrlInBrowser(info.remoteUrl)
	end
end

provider.processRenderedPhotos = PublishTask.processRenderedPhotos
provider.viewForCollectionSettings = PublishTask.viewForCollectionSettings
provider.updateCollectionSettings = PublishTask.updateCollectionSettings
provider.renamePublishedCollection = PublishTask.renamePublishedCollection
provider.deletePublishedCollection = PublishTask.deletePublishedCollection
provider.deletePhotosFromPublishedCollection = PublishTask.deletePhotosFromPublishedCollection
provider.imposeSortOrderOnPublishedCollection = PublishTask.imposeSortOrderOnPublishedCollection

return provider
