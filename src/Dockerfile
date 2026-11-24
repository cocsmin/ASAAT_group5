# One image recipe for every system: docker build --build-arg SERVICE=thm .
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${SERVICE}

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget && mkdir /data && chown 65532:65532 /data
COPY --from=build /out/service /service
USER 65532:65532
WORKDIR /data
ENTRYPOINT ["/service"]
