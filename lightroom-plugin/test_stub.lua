-- Shared Lightroom SDK stub for the plug-in self-checks. Run the tests from
-- this directory: lua test_<name>.lua
--
-- Loads plug-in modules outside Lightroom by providing a global `import`
-- backed by the `sdk` table below, and a global `log`. Tests replace the
-- pieces they need (LrHttp.post, activeCatalog ...). Sleeps are recorded,
-- not performed, so the retry ladder costs nothing.
package.path = "smugbox.lrplugin/?.lua;" .. package.path

local stub = {}

stub.slept = {}
stub.dialogs = {}

stub.sdk = {
	LrHttp = {},
	LrPathUtils = { leafName = function(p) return p end },
	LrTasks = {
		pcall = pcall,
		sleep = function(n) table.insert(stub.slept, n) end,
	},
	LrDialogs = {
		message = function(title, msg, kind)
			table.insert(stub.dialogs, { title = title, message = msg, kind = kind })
		end,
	},
	LrErrors = {
		throwUserError = function(msg) error(msg, 0) end,
	},
	LrApplication = {},
	LrDate = {},
	LrView = {},
	LrProgressScope = function()
		return { setPortionComplete = function() end, isCanceled = function() return false end, done = function() end }
	end,
	LrUUID = {},
}

function import(name)
	return stub.sdk[name]
end

log = {
	tracef = function() end,
	warnf = function() end,
	infof = function() end,
	error = function() end,
}

-- Forgets recorded sleeps and dialogs between cases.
function stub.reset()
	stub.slept = {}
	stub.dialogs = {}
end

-- Makes LrApplication.activeCatalog() return `catalog`, filling in the
-- methods the plug-in calls with pass-through defaults.
function stub.catalog(catalog)
	catalog = catalog or {}
	catalog.withWriteAccessDo = catalog.withWriteAccessDo or function(_, _, fn) fn() end
	catalog.getPublishedCollectionByLocalIdentifier = catalog.getPublishedCollectionByLocalIdentifier or function() return nil end
	stub.sdk.LrApplication.activeCatalog = function() return catalog end
	return catalog
end

-- Installs LrHttp.get/post handlers. `handler(method, url, body)` returns
-- status, responseBody. Every call is appended to stub.calls as
-- { method, url, body }.
stub.calls = {}
function stub.http(handler)
	stub.calls = {}
	local function record(method, url, body)
		table.insert(stub.calls, { method = method, url = url, body = body })
		local status, resp = handler(method, url, body)
		if status == nil then
			return nil, nil -- no response at all
		end
		return resp or "", { status = status }
	end
	stub.sdk.LrHttp.get = function(url) return record("GET", url, nil) end
	stub.sdk.LrHttp.post = function(url, body, _, method) return record(method or "POST", url, body) end
	stub.sdk.LrHttp.postMultipart = function(url) return record("POST", url, nil) end
end

return stub
