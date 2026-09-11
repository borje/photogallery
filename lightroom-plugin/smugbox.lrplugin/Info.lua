--[[
Smugbox publish plug-in for Lightroom Classic.

Publishes collections to the self-hosted Go gallery backend in this
repository (see /backend). One published collection = one album.
]]

return {
	LrSdkVersion = 3.0,
	LrSdkMinimumVersion = 3.0, -- publish services exist since SDK 3.0

	LrToolkitIdentifier = "se.granberg.gallerypublish",
	LrPluginName = "Smugbox",
	LrPluginInfoUrl = "https://github.com/bege/photogallery",

	LrInitPlugin = "Init.lua",
	LrPluginInfoProvider = "PluginInfoProvider.lua",

	LrExportServiceProvider = {
		title = "Smugbox",
		file = "PublishServiceProvider.lua",
	},

	VERSION = { major = 0, minor = 1, revision = 0, build = 0 },
}
