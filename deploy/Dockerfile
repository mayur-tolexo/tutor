# Build the PWA, then the Go binary that embeds it, then ship a minimal image.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tutor ./cmd/tutor

# python3 is present only so `tutor content validate` can run in this image;
# the server never executes student code locally when NEEV_API_KEY is set.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates python3 && adduser -D -u 10001 tutor
COPY --from=build /out/tutor /usr/local/bin/tutor
USER tutor
EXPOSE 8080
ENTRYPOINT ["tutor"]
CMD ["serve"]
