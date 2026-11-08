# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cookserver ./cmd/cookserver

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cookserver /cookserver
ENV DATA_DIR=/data ADDR=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/cookserver", "healthcheck"]
ENTRYPOINT ["/cookserver"]
CMD ["serve"]
