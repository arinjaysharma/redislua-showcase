-- Atomic wallet transfer with transaction log
local amount = tonumber(ARGV[1])
local txn_id = ARGV[2]

local from_balance = tonumber(redis.call('GET', KEYS[1]) or '0')

if from_balance < amount then
    return {-1, from_balance, 0}
end

local new_from = tonumber(redis.call('DECRBY', KEYS[1], amount))
local new_to   = tonumber(redis.call('INCRBY', KEYS[2], amount))

local log_entry = txn_id .. '|' .. amount .. '|' .. new_from .. '|' .. new_to
redis.call('LPUSH', KEYS[3], log_entry)
redis.call('LTRIM', KEYS[3], 0, 49)

return {1, new_from, new_to}
