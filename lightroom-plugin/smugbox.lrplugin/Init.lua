--[[
Runs once when the plug-in loads. Sets up the shared logger and preferences.

Other files reach these through the globals `log` and `prefs`; Lightroom gives
every plug-in its own global environment so this does not leak anywhere.
]]

local LrLogger = import "LrLogger"
local LrPrefs = import "LrPrefs"

local prefs = LrPrefs.prefsForPlugin()
if prefs.logging == nil then
	prefs.logging = true
end

local log = LrLogger("SmugboxPublish")
if prefs.logging then
	log:enable("logfile")
else
	log:disable()
end

_G.log = log
_G.prefs = prefs

log:info("Smugbox plug-in loaded")
