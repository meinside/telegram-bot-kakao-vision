# telegram-bot-kakao-vision

This Telegram Bot was built for showing how to use [Go wrapper for Kakao Vision API](https://github.com/meinside/kakao-api-go).

Slightly modified from my previous project: [MS Cognitive API Bot](https://github.com/meinside/telegram-ms-cognitive-bot).

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

### A. Just run it

After all things are setup correctly, just run the built binary:

```bash
$ ./telegram-bot-kakao-vision
```

### B. Run as a systemd Service

#### a. systemd

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

### C. Run with docker

Build a docker image:

```bash
$ docker build -t telegram-bot-kakao-vision:latest .
```

then run it with:

```bash
$ docker run --restart=always telegram-bot-kakao-vision:latest
```

### Run with docker-compose

```bash
$ docker-compose build
```

and then start with:

```bash
$ docker-compose up -d
```

## Tips

You can remove intermediate images with:

```bash
$ docker rmi $(docker images --filter "dangling=true" -q --no-trunc)
```

## Sample

* [@kakao_vision_api_bot](https://telegram.me/kakao_vision_api_bot) is being run on my tiny Raspberry Pi server, so **don't be mad if it doesn't respond to your messages.**

## License

MIT

