FROM golang:1.17

WORKDIR /go/src/github.com/rdbell/nvote
COPY . .

RUN go install -v -ldflags "-X main.cacheBuster=$(date +%s)"

CMD ["nvote"]
