-- Flash-sale oversell prevention
local qty   = tonumber(ARGV[1])
local stock = redis.call('GET', KEYS[1])

if stock == false then
    return {-2, 0}
end

stock = tonumber(stock)

if stock < qty then
    return {-1, stock}
end

local remaining = tonumber(redis.call('DECRBY', KEYS[1], qty))
return {remaining, remaining}
