-- Self-check for the upload retry in SmugboxAPI. Run: lua test_retry.lua
-- Stubs the Lightroom SDK so the module can be loaded outside Lightroom.
package.path = "smugbox.lrplugin/?.lua;" .. package.path

local slept = {}
local sdk = {
	LrHttp = {},
	LrPathUtils = { leafName = function(p) return p end },
	LrTasks = {
		pcall = pcall,
		sleep = function(n) table.insert(slept, n) end,
	},
}
function import(name) return sdk[name] end
log = { tracef = function() end, warnf = function() end, infof = function() end }

local SmugboxAPI = require "SmugboxAPI"

-- Replies is a list of { status, body }; each upload attempt takes the next.
local function run(replies, isCanceled)
	slept = {}
	local attempts = 0
	sdk.LrHttp.postMultipart = function()
		attempts = attempts + 1
		local r = replies[math.min(attempts, #replies)]
		return r.body, { status = r.status }
	end
	local api = SmugboxAPI.new("https://example.test", "key")
	api.isCanceled = isCanceled
	local ok = api:uploadPhoto("album", "/tmp/x.jpg", { filename = "x.jpg" })
	return ok, attempts
end

local good = { status = 200, body = '{"id":"1"}' }
local bare404 = { status = 404, body = "404 page not found" }
local coded404 = { status = 404, body = '{"error":"album_not_found"}' }

-- A bare 404 from the proxy is retried until the backend answers again.
local ok, n = run({ bare404, bare404, good })
assert(ok and n == 3, "bare 404 should retry: " .. tostring(ok) .. " " .. n)
assert(slept[1] == 5 and slept[2] == 15, "backoff should grow")

-- A 404 the backend itself sent is an answer, not an outage.
ok, n = run({ coded404 })
assert(not ok and n == 1, "coded 404 should not retry: " .. n)

-- Rejected credentials are not worth repeating either.
ok, n = run({ { status = 401, body = "" } })
assert(not ok and n == 1, "401 should not retry: " .. n)

-- Server errors are, and give up after the last delay.
ok, n = run({ { status = 502, body = "" } })
assert(not ok and n == 6, "502 should exhaust the delays: " .. n)

-- Cancelling stops the loop before the next wait.
ok, n = run({ bare404 }, function() return true end)
assert(not ok and n == 1 and #slept == 0, "cancel should stop retrying: " .. n)

print("ok")
