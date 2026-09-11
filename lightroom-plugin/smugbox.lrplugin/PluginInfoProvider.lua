--[[
Plug-in Manager panel: version, logging toggle and a shortcut to the log.
]]

local LrShell = import "LrShell"
local LrView = import "LrView"

local Util = require "Util"

local bind = LrView.bind

return {
	sectionsForTopOfDialog = function(f, propertyTable)
		propertyTable.logging = prefs.logging and true or false
		return {
			{
				title = "Smugbox",
				bind_to_object = propertyTable,
				f:row {
					f:static_text { title = "Publishes collections as albums to your self-hosted gallery." },
				},
				f:row {
					f:checkbox { title = "Write a log file (SmugboxPublish.log)", value = bind "logging" },
				},
				f:row {
					f:static_text { title = Util.logFilePath(), truncation = "middle", fill_horizontal = 1 },
					f:push_button {
						title = "Show log folder",
						action = function()
							LrShell.revealInShell(Util.logFolder())
						end,
					},
				},
			},
		}
	end,

	endDialog = function(propertyTable)
		prefs.logging = propertyTable.logging and true or false
		if prefs.logging then
			log:enable("logfile")
		else
			log:disable()
		end
	end,
}
