-- O(1) Token Bucket Rate Limiter
-- KEYS[1] : rate limit key (hash)
-- ARGV[1] : rate (tokens per second)
-- ARGV[2] : burst capacity
-- ARGV[3] : current timestamp in microseconds
-- ARGV[4] : tokens requested (usually 1)

local key       = KEYS[1]
local rate      = tonumber(ARGV[1])
local capacity  = tonumber(ARGV[2])
local now_us    = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local data = redis.call('HMGET', key, 'tokens', 'last_us')
local tokens = tonumber(data[1])
local last_us = tonumber(data[2])

if not tokens then
    tokens = capacity
    last_us = now_us
else
    if now_us > last_us then
        local elapsed_s = (now_us - last_us) / 1000000.0
        tokens = math.min(capacity, tokens + elapsed_s * rate)
        last_us = now_us
    end
end

if tokens >= requested then
    tokens = tokens - requested
    redis.call('HMSET', key, 'tokens', tokens, 'last_us', last_us)
    redis.call('PEXPIRE', key, 10000)
    return {1, math.floor(tokens)}
else
    redis.call('HMSET', key, 'tokens', tokens, 'last_us', last_us)
    redis.call('PEXPIRE', key, 10000)
    return {0, math.floor(tokens)}
end