# Dockerfile for Golang application

# Arguments
ARG CONF_FILE=config.json

# Temporary image for building
FROM meinside/alpine-golang:latest AS builder

# Add unprivileged user/group
RUN mkdir /user && \
	echo 'nobody:x:65534:65534:nobody:/:' > /user/passwd && \
	echo 'nobody:x:65534:' > /user/group

# Install certs, git, and mercurial
RUN apk add --no-cache ca-certificates git mercurial

# Working directory outside $GOPATH
WORKDIR /src

# Copy go module files and download dependencies
COPY ./go.mod ./go.sum ./
RUN go mod download

# Copy source files
COPY ./ ./

# Build source files statically
RUN CGO_ENABLED=0 go build \
	-installsuffix 'static' \
	-o /app \
	.

# Minimal image for running the application
FROM scratch as final

# Copy files from temporary image
COPY --from=builder /user/group /user/passwd /etc/
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app /

# Copy config file
COPY ./${CONF_FILE} /

# Open ports (if needed)
#EXPOSE 8080
#EXPOSE 80
#EXPOSE 443

# Will run as unprivileged user/group
USER nobody:nobody

# Entry point for the built application
ENTRYPOINT ["/app"]
