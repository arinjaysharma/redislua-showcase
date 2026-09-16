FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /redislua-showcase .

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /redislua-showcase .
EXPOSE 8080
CMD ["./redislua-showcase"]
