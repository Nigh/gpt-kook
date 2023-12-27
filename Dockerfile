FROM golang:alpine AS builder
WORKDIR $GOPATH/src/mypackage/myapp/
COPY ./src/* ./
RUN go mod tidy
RUN GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app

FROM scratch
COPY --from=builder /app /app
VOLUME /config
ENTRYPOINT ["/app"]
