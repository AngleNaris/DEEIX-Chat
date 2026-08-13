FROM golang:1.23-alpine AS build
WORKDIR /src
COPY tools/mm-isolation/go.mod tools/mm-isolation/go.sum ./
RUN go mod download
COPY tools/mm-isolation/*.go ./
RUN go build -trimpath -ldflags='-s -w' -o /mm-isolation .

FROM alpine:3.20
COPY --from=build /mm-isolation /usr/local/bin/mm-isolation
ENTRYPOINT ["/usr/local/bin/mm-isolation"]
