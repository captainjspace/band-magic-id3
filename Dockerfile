# band-magic-id3-api for Cloud Run. Pure Go, no cgo, so a static image.
# Compiles on the build host's arch and cross-compiles to --platform's.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -buildvcs=false -trimpath -ldflags="-s -w" -o /id3-api ./cmd/band-magic-id3-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /id3-api /id3-api
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/id3-api"]
