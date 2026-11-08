# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/fwump38/hearth/internal/web.Version=${VERSION}" -o /out/cookserver ./cmd/cookserver

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cookserver /cookserver
ENV DATA_DIR=/data ADDR=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/cookserver", "healthcheck"]
ENTRYPOINT ["/cookserver"]
CMD ["serve"]
