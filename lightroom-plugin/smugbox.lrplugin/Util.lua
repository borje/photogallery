--[[
Small helpers shared by the plug-in files.
]]

local LrApplication = import "LrApplication"
local LrPathUtils = import "LrPathUtils"
local LrTasks = import "LrTasks"
local LrFileUtils = import "LrFileUtils"

local Util = {}

function Util.trim(s)
	if s == nil then
		return ""
	end
	return (tostring(s):gsub("^%s+", ""):gsub("%s+$", ""))
end

-- Deletes a rendered temp file; Lightroom cleans up later anyway, but this
-- keeps disk use down during large publishes.
function Util.safeDelete(path)
	if type(path) ~= "string" or path == "" then
		return
	end
	local ok, err = LrTasks.pcall(function()
		if LrFileUtils.exists(path) then
			LrFileUtils.delete(path)
		end
	end)
	if not ok then
		log:warnf("could not delete temp file %s: %s", path, tostring(err))
	end
end

-- Folder where LrLogger writes SmugboxPublish.log.
function Util.logFolder()
	local home = LrPathUtils.getStandardFilePath("home")
	local major = 0
	local ok, version = LrTasks.pcall(function()
		return LrApplication.versionTable()
	end)
	if ok and version and version.major then
		major = version.major
	end
	if major >= 14 then
		if MAC_ENV then
			return LrPathUtils.child(home, "Library/Logs/Adobe/Lightroom/LrClassicLogs")
		end
		return LrPathUtils.child(home, "AppData\\Local\\Adobe\\Lightroom\\Logs\\LrClassicLogs")
	end
	return LrPathUtils.child(LrPathUtils.getStandardFilePath("documents"), "LrClassicLogs")
end

function Util.logFilePath()
	return LrPathUtils.child(Util.logFolder(), "SmugboxPublish.log")
end

return Util
