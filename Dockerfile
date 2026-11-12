FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go help.go config.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /kkk main.go help.go config.go

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /kkk /app/kkk
ENTRYPOINT ["/app/kkk"]
