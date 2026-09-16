-- Sliding-window rate limiter
local key    = KEYS[1]
local now    = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit  = tonumber(ARGV[3])
local uid    = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)

local count = redis.call('ZCARD', key)

if count < limit then
    redis.call('ZADD', key, now, uid)
    redis.call('PEXPIRE', key, window)
    return {1, limit - count - 1, now + window}
else
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local reset_at = now + window
    if #oldest >= 2 then
        reset_at = tonumber(oldest[2]) + window
    end
    return {0, 0, reset_at}
end
