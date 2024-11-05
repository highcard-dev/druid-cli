function string.fromhex(str)
    return (str:gsub('..', function(cc)
        return string.char(tonumber(cc, 16))
    end))
end

function string.tohex(str)
    return (str:gsub('.', function(c)
        return string.format('%02X', string.byte(c))
    end))
end

function handle(ctx, data)

    -- prtocol begins with FFFFFFFF and the packedid

    -- get packet index

    -- check if start with FFFFFFFF

    hex = string.tohex(data)

    if string.sub(hex, 1, 8) ~= "FFFFFFFF" then
        debug_print("Invalid Packet " .. hex)
        return
    end

    packetId = string.sub(hex, 9, 10)

    payload = string.sub(hex, 11)

    -- check if packet is 54

    debug_print("Packet ID: " .. packetId)

    if packetId == "55" then

        if payload == "FFFFFFFF" or payload == "00000000" then
            debug_print("Received Packet: " .. hex)
            resHex = string.fromhex("FFFFFFFF414BA1D522") -- this is not good, as we allways pass the same key for the challenge
            ctx.sendData(resHex)
            return
        end

        if payload == "4BA1D522" then
            debug_print("Received Packet: " .. hex)
            resHex = string.fromhex("FFFFFFFF4400") -- this is not good to be hardcoded, but fine for now

            ctx.sendData(resHex)
            return
        end
        debug_print("Bad challenge: " .. hex)
        return
    end

    if packetId == "56" then

        if payload == "FFFFFFFF" or payload == "00000000" then
            debug_print("Received Packet: " .. hex)
            resHex = string.fromhex("FFFFFFFF414BA1D522") -- this is not good, as we allways pass the same key for the challenge
            ctx.sendData(resHex)
            return
        end

        if payload == "4BA1D522" then
            debug_print("Received Packet: " .. hex)
            resHex = string.fromhex(
                "FFFFFFFF451A00414C4C4F57444F574E4C4F414443484152535F69003100414C4C4F57444F574E4C4F41444954454D535F69003100436C757374657249645F73004B4150323032326E76637738393233386E3332726677653900435553544F4D5345525645524E414D455F73006B617020707670202F20342D6D616E202F2078352D783235202F20776F726B65727320667269656E646C79207365727665720044617954696D655F730037360047616D654D6F64655F73005465737447616D654D6F64655F43004841534143544956454D4F44535F690031004C45474143595F690030004D4154434854494D454F55545F66003132302E303030303030004D4F44305F7300323839373838353837383A4544393730443545343845324143433334333545374339373345434135373637004D4F44315F7300323536343534363435353A3934413336414236343933453241443335364631343142313932383633453445004D4F44325F7300333034363539363536343A3832453245393730343446444139463642464237353439443730433337423133004D4F44335F7300313939393434373137323A3836453432424644343646453430363338443639344141384342453634344134004D6F6449645F6C0030004E6574776F726B696E675F690030004E554D4F50454E505542434F4E4E003530004F4646494349414C5345525645525F690030004F574E494E474944003930323032313035363131373133353337004F574E494E474E414D45003930323032313035363131373133353337005032504144445200393032303231303536313137313335333700503250504F52540037373837005345415243484B4559574F5244535F7300437573746F6D0053657276657250617373776F72645F620066616C73650053455256455255534553424154544C4559455F6200747275650053455353494F4E464C41475300313730370053455353494F4E49535056455F69003000") -- this is not good to be hardcoded, but fine for now

            ctx.sendData(resHex)
            return
        end
        debug_print("Bad challenge: " .. hex)
        return
    end

    if packetId == "54" then

        name = get_var("ServerListName") or "Coldstarter is cool (server is idle, join to start)"

        if get_finish_sec() ~= nil then
            nameTemplate = get_var("ServerListNameStarting") or "Druid Gameserver (starting) - %ds"
            name = string.format(nameTemplate, math.ceil(get_finish_sec()))
        end

        map = get_var("MapName") or "TheIsland"

        folder = get_var("GameSteamFolder") or "ark_survival_evolved"

        gameName = get_var("GameName") or "ARK: Survival Evolved"

        steamIdString = get_var("GameSteamId") or "0"

        steamId = tonumber(steamIdString)

        serverPort = get_port("main")

        -- hex
        nameHex = string.tohex(name)

        mapHex = string.tohex(map)

        folderHex = string.tohex(folder) -- ark: ark_survival_evolved

        steamIdHex = number_to_little_endian_short(steamId)

        gameHex = string.tohex(gameName)

        maxPlayerHex = "00"
        playerHex = "00"
        botHex = "00"

        serverTypeHex = "64" -- dedicated

        osHex = "6C" -- l (6C) for linux, w (77) for windows

        vacHex = "01" -- 01 for secure, 00 for insecure

        version = string.tohex("1.0.0.0")

        -- EDF & 0x80: Port
        -- EDF & 0x10: SteamID
        -- EDF & 0x20 Keywords
        -- EDF & 0x01 GameID

        edfFlagHex = "B1"

        -- short as hex
        gamePortHex = number_to_little_endian_short(serverPort)

        steamId = "01D075C44C764001"

        tags =
            ",OWNINGID:90202064633057281,OWNINGNAME:90202064633057281,NUMOPENPUBCONN:50,P2PADDR:90202064633057281,P2PPORT:" ..
                serverPort .. ",LEGACY_i:0"

        tagsHex = string.tohex(tags)

        edfHex = gamePortHex .. steamId .. tagsHex .. "00" .. "FE47050000000000"

        res =
            "FFFFFFFF4911" .. nameHex .. "00" .. mapHex .. "00" .. folderHex .. "00" .. gameHex .. "00" .. steamIdHex ..
                playerHex .. maxPlayerHex .. botHex .. serverTypeHex .. osHex .. vacHex .. version .. "00" .. edfFlagHex ..
                edfHex

        debug_print("Response length: " .. string.len(tags))

        resHex = string.fromhex(res)

        ctx.sendData(resHex)
        return
    end

    debug_print("Unknown Packet: " .. hex)

end

function number_to_little_endian_short(num)
    -- Ensure the number is in the 16-bit range for unsigned short
    if num < 0 or num > 65535 then
        error("Number " .. num .. " out of range for 16-bit unsigned short")
    end

    -- Convert the number to two bytes in little-endian format
    local low_byte = num % 256 -- Least significant byte
    local high_byte = math.floor(num / 256) % 256 -- Most significant byte

    -- Format as hexadecimal string
    return string.format("%02X%02X", low_byte, high_byte)
end
