FROM golang:1.17

WORKDIR /go/src/github.com/rdbell/nvote
COPY . .

RUN go get github.com/tdewolff/minify/cmd/minify
RUN ./minify.sh
RUN go install -v -ldflags "-X main.cacheBuster=$(date +%s)"

CMD ["nvote"]
