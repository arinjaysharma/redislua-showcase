-- Atomic leaderboard update + consistent rank snapshot
local board   = KEYS[1]
local player  = ARGV[1]
local score   = tonumber(ARGV[2])
local topN    = tonumber(ARGV[3])
local nbRange = tonumber(ARGV[4])

redis.call('ZADD', board, score, player)

local rank = redis.call('ZREVRANK', board, player)

local topList = redis.call('ZREVRANGE', board, 0, topN - 1, 'WITHSCORES')

local startIdx = math.max(0, rank - nbRange)
local endIdx   = rank + nbRange
local neighbors = redis.call('ZREVRANGE', board, startIdx, endIdx, 'WITHSCORES')

local total = redis.call('ZCARD', board)

return {rank + 1, score, total, topList, neighbors}
