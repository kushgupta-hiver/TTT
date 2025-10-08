# ---------- build stage ----------
    FROM golang:1.25-alpine AS builder
    WORKDIR /src
    
    # Enable module mode and static build
    ENV CGO_ENABLED=0 GO111MODULE=on
    
    # Cache deps first
    COPY go.mod go.sum ./
    RUN go mod download
    
    # Copy the rest
    COPY . .
    
    # Build the server binary
    RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
    
    # ---------- runtime stage ----------
    FROM gcr.io/distroless/static-debian12:nonroot
    WORKDIR /app
    
    # Copy binary
    COPY --from=builder /out/server /app/server
    
    # Port & environment defaults
    EXPOSE 8000
    ENV ADDR=":8000" \
        GRACE_SECONDS="30"
    
    # Run as nonroot distroless user
    USER nonroot:nonroot
    
    ENTRYPOINT ["/app/server"]


# delete container
# docker rm -f ttt

# delete image
# docker rmi ttt-ws:latest

# build image
# docker build -t ttt-ws:latest .

# run image
# docker run --rm -p 8000:8000 -e ADDR=":8000" --name ttt ttt-ws:latest