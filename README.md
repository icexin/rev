# Overview

Reverse tcp proxy

# Usage

## server mode

listen on port `8421`

`rev -s -addr=:8421`

## client mode

server will listen on 9000 and proxy connection to localhost:8080

`rev -addr=$serverip:8421 -p 8080:9000`

`-p` can be used multiple times

