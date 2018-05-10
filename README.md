# telegram-bot-kakao-vision

This Telegram Bot was built for showing how to use [Go wrapper for Kakao Vision API](https://github.com/meinside/kakao-api-go).

## Preparation

Install essential libraries and packages:

```bash
# for freetype font and image manipulation
$ sudo apt-get install libgl1-mesa-dev
$ go get github.com/golang/freetype/...
$ go get github.com/llgcode/draw2d/...
$ go get github.com/disintegration/gift

# for telegram bot api
$ go get github.com/meinside/telegram-bot-go

# for kakao rest api for vision
$ go get github.com/meinside/kakao-api-go/...

# for loggly
$ go get github.com/meinside/loggly-go
```

## Install & Build

```bash
$ git clone https://github.com/meinside/telegram-bot-kakao-vision
$ cd telegram-bot-kakao-vision/
$ go build
```

## How to Configure

Copy the sample config file and fill it with your values:

```bash
$ cp config.json.sample config.json
$ vi config.json
```

For example:

```json
{
	"telegram-api-token": "0123456789:AaBbCcDdEeFfGgHhIiJj_klmnopqrstuvwx-yz",
	"telegram-monitor-interval-seconds": 1,
	"kakao-rest-api-key": "abcdefghijklmnopqrstuvwxyz0123456789",
	"is-verbose": false
}
```

## How to Run

After all things are setup correctly, just run the built binary:

```bash
$ ./telegram-bot-kakao-vision
```

## How to Run as a Service

### a. systemd

```bash
$ sudo cp systemd/telegram-bot-kakao-vision.service /lib/systemd/system/
$ sudo vi /lib/systemd/system/telegram-bot-kakao-vision.service
```

and edit **User**, **Group**, **WorkingDirectory** and **ExecStart** values.

It will launch automatically on boot with:

```bash
$ sudo systemctl enable telegram-bot-kakao-vision.service
```

and will start with:

```bash
$ sudo systemctl start telegram-bot-kakao-vision.service
```

## License

MIT

