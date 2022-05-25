compile:
	echo "Compiling for every OS and Platform"
	GOOS=freebsd GOARCH=386 go build -o build/supportpal-exporter-freebsd-386 exporter.go
	GOOS=freebsd GOARCH=amd64 go build -o build/supportpal-exporter-freebsd-amd64 exporter.go
	GOOS=linux GOARCH=386 go build -o build/supportpal-exporter-386 exporter.go
	GOOS=linux GOARCH=amd64 go build -o build/supportpal-exporter-amd64 exporter.go

all: compile