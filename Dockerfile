FROM golang:1.26-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /crate ./cmd/crate/ && \
    CGO_ENABLED=0 go build -o /provider-musicbrainz ./cmd/provider-musicbrainz/ && \
    CGO_ENABLED=0 go build -o /provider-deezer ./cmd/provider-deezer/

FROM alpine:3.21
ENV CRATE_DB_PATH=/app/data/crate.db
ENV CRATE_CACHE_PATH=/app/data/cache.db
ENV CRATE_ACTIVITY_PATH=/app/data/activity.db
ENV CRATE_LIBRARY_PATH=/app/library
ENV CRATE_PROVIDERS=musicbrainz:./provider-musicbrainz:50051,deezer:./provider-deezer:50052
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /crate /provider-musicbrainz /provider-deezer ./
EXPOSE 6969
VOLUME ["/app/data", "/app/library"]
ENTRYPOINT ["./crate"]
