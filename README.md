# go-kiwisdr-client
Client for Kiwi SDR in golang

See comments and flags in kiwi-listen.go.

### Example
    go run kiwi-listen.go --freq=740000 --mode=am --duration=5s \
        --kiwi=sybil.yak.net --printinfo |
      paplay --rate=12000 --format=s16le --channels=1 --raw /dev/stdin
