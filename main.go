package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	// for using .ttf
	"github.com/disintegration/gift"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/llgcode/draw2d/draw2dimg"

	// kakao rest api
	kakaoapi "github.com/meinside/kakao-api-go"

	// for Telegram bot
	bot "github.com/meinside/telegram-bot-go"

	// for logging on Loggly
	"github.com/meinside/loggly-go"
)

var client *bot.Bot
var logger *loggly.Loggly

const (
	appName = "KakaoVisionBot"
)

// logglyLog struct
type logglyLog struct {
	Application string      `json:"app"`
	Severity    string      `json:"severity"`
	Timestamp   string      `json:"timestamp"`
	Message     string      `json:"message,omitempty"`
	Object      interface{} `json:"obj,omitempty"`
}

// VisionCommand type
type VisionCommand string

// XXX - When a new command is added, add it here too.
const (
	DetectFaces    VisionCommand = "Detect Faces"
	DetectProducts VisionCommand = "Detect Products"
	DetectNSFW     VisionCommand = "Detect NSFW "
	Tag            VisionCommand = "Tag This Image"
	AnalyzePoses   VisionCommand = "Analyze Poses"
	ExtractTexts   VisionCommand = "Extract Texts"

	// fun commands
	MaskFaces VisionCommand = "Mask Faces"

	None VisionCommand = ""
)

// XXX - When a new command is added, add it here too.
var allCmds = map[VisionCommand]string{
	DetectFaces:    "detect_faces",
	DetectProducts: "detect_products",
	DetectNSFW:     "detect_nsfw",
	Tag:            "tag",
	AnalyzePoses:   "analyze_poses",
	ExtractTexts:   "extract_texts",

	// fun commands
	MaskFaces: "mask_faces",
}

func visionCommandForCommand(cmd string) (result VisionCommand) {
	result = None

	for k, v := range allCmds {
		if v == cmd {
			result = k
			break
		}
	}

	return result

}

var fileIDs = map[string]string{}

var kakaoClient *kakaoapi.Client

var font *truetype.Font

const (
	messageActionImage     = "Choose action for this image:"
	messageUnprocessable   = "Unprocessable message."
	messageFailedToGetFile = "Failed to get file from the server."
	messageCanceled        = "Canceled."
	messageHelp            = `Send any image to this bot, then select one of the following actions:

- Detect Faces
- Detect Products
- Detect NSFW
- Tag This Image
- Analyze Poses
- Extract Texts
- Mask Faces

then it will send the result message and/or image back to you.

* Github: https://github.com/meinside/telegram-bot-kakao-vision
`

	commandCancel = "cancel"

	fontFilepath = "fonts/RobotoCondensed-Regular.ttf"
)

// constants for drawing
const (
	CircleRadius = 0.5
	StrokeWidth  = 1.5

	PosePointRadius = 2.0
	PoseStrokeWidth = 1.5
)

// colors
var colors = []color.RGBA{
	color.RGBA{255, 255, 0, 255}, // yellow
	color.RGBA{0, 255, 255, 255}, // cyan
	color.RGBA{255, 0, 255, 255}, // purple
	color.RGBA{0, 255, 0, 255},   // green
	color.RGBA{0, 0, 255, 255},   // blue
	color.RGBA{255, 0, 0, 255},   // red
}
var maskColor = color.RGBA{0, 0, 0, 255} // black

// config file's name
const (
	configFilename = "config.json"
)

// Config struct
type Config struct {
	TelegramAPIToken               string `json:"telegram-api-token"`
	TelegramMonitorIntervalSeconds int    `json:"telegram-monitor-interval-seconds"`
	KakaoAPIKey                    string `json:"kakao-rest-api-key"`
	LogglyToken                    string `json:"loggly-token,omitempty"`
	IsVerbose                      bool   `json:"is-verbose"`
}

var conf Config

func pwd() string {
	if execFilepath, err := os.Executable(); err == nil {
		return filepath.Dir(execFilepath)
	}

	return "." // fallback
}

func init() {
	pwd := pwd()

	// read from config file
	if file, err := ioutil.ReadFile(filepath.Join(pwd, configFilename)); err != nil {
		panic(err)
	} else {
		if err := json.Unmarshal(file, &conf); err != nil {
			panic(err)
		}
	}

	// check values
	if conf.TelegramMonitorIntervalSeconds <= 0 {
		conf.TelegramMonitorIntervalSeconds = 1
	}

	// kakao api client
	kakaoClient = kakaoapi.NewClient(conf.KakaoAPIKey)
	kakaoClient.Verbose = conf.IsVerbose

	// telegram bot client
	client = bot.NewClient(conf.TelegramAPIToken)
	client.Verbose = conf.IsVerbose

	// loggly logger client
	if conf.LogglyToken != "" {
		logger = loggly.New(conf.LogglyToken)
	}

	// others
	bytes, err := ioutil.ReadFile(filepath.Join(pwd, fontFilepath))
	if err == nil {
		var f *truetype.Font
		f, err = truetype.Parse(bytes)
		if err == nil {
			font = f
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}
}

func main() {
	// catch SIGINT and SIGTERM and terminate gracefully
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		os.Exit(1)
	}()

	// get info about this bot
	if me := client.GetMe(); me.Ok {
		logMessage(fmt.Sprintf("Starting bot: @%s (%s)", *me.Result.Username, me.Result.FirstName))

		// delete webhook (getting updates will not work when wehbook is set up)
		if unhooked := client.DeleteWebhook(); unhooked.Ok {
			// wait for new updates
			client.StartMonitoringUpdates(
				0,
				conf.TelegramMonitorIntervalSeconds,
				func(b *bot.Bot, update bot.Update, err error) {
					if err == nil {
						if update.HasMessage() {
							processUpdate(b, update) // process message
						} else if update.HasCallbackQuery() {
							processCallbackQuery(b, update) // process callback query
						} else {
							logError("Update not processable")
						}
					} else {
						logError(fmt.Sprintf("Error while receiving update (%s)", err))
					}
				},
			)
		} else {
			panic("Failed to delete webhook")
		}
	} else {
		panic("Failed to get info of the bot")
	}
}

// log message
func logMessage(message string) {
	log.Println(message)

	if logger != nil {
		_, timestamp := loggly.Timestamp()

		logger.Log(logglyLog{
			Application: appName,
			Severity:    "Log",
			Timestamp:   timestamp,
			Message:     message,
		})
	}
}

// log error message
func logError(message string) {
	log.Println(message)

	if logger != nil {
		_, timestamp := loggly.Timestamp()

		logger.Log(logglyLog{
			Application: appName,
			Severity:    "Error",
			Timestamp:   timestamp,
			Message:     message,
		})
	}
}

// log request from user
func logRequest(username, fileURL string, command VisionCommand) {
	if logger != nil {
		_, timestamp := loggly.Timestamp()

		logger.Log(logglyLog{
			Application: appName,
			Severity:    "Verbose",
			Timestamp:   timestamp,
			Object: struct {
				Username string        `json:"username"`
				FileURL  string        `json:"file_url"`
				Command  VisionCommand `json:"command"`
			}{
				Username: username,
				FileURL:  fileURL,
				Command:  command,
			},
		})
	}
}

// process incoming update from Telegram
func processUpdate(b *bot.Bot, update bot.Update) bool {
	result := false // process result

	var message string
	options := bot.OptionsSendMessage{}.SetReplyToMessageID(update.Message.MessageID)

	if update.Message.HasPhoto() {
		options.SetReplyMarkup(bot.InlineKeyboardMarkup{
			InlineKeyboard: genImageInlineKeyboards(update.Message.LargestPhoto().FileID),
		})
		message = messageActionImage
	} else if update.Message.HasDocument() && strings.HasPrefix(*update.Message.Document.MimeType, "image/") {
		options.SetReplyMarkup(bot.InlineKeyboardMarkup{
			InlineKeyboard: genImageInlineKeyboards(update.Message.Document.FileID),
		})
		message = messageActionImage
	} else {
		message = messageHelp
	}

	// send message
	if sent := b.SendMessage(update.Message.Chat.ID, message, options); sent.Ok {
		result = true
	} else {
		logError(fmt.Sprintf("Failed to send message: %s", *sent.Description))
	}

	return result
}

// process incoming callback query
func processCallbackQuery(b *bot.Bot, update bot.Update) (result bool) {
	// process result
	result = false

	var username string
	message := ""
	query := *update.CallbackQuery
	data := *query.Data

	if data == commandCancel {
		message = messageCanceled
	} else {
		parsedCommand := strings.Split(data, "/")

		if len(parsedCommand) >= 2 {
			command := parsedCommand[0]
			shortenedFileID := parsedCommand[1]

			if fileID, exists := fileIDs[shortenedFileID]; exists {
				if fileResult := b.GetFile(fileID); fileResult.Ok {
					fileURL := b.GetFileURL(*fileResult.Result)

					if strings.Contains(*query.Message.Text, "image") {
						visionCommand := visionCommandForCommand(command)

						go processImage(b, query.Message.Chat.ID, query.Message.MessageID, fileURL, visionCommand)

						message = fmt.Sprintf("Processing '%s' on received image...", visionCommand)

						// log request
						if query.From.Username == nil {
							username = query.From.FirstName
						} else {
							username = *query.From.Username
						}
						logRequest(username, fileURL, visionCommand)
					} else {
						message = messageUnprocessable
					}
				} else {
					logError(fmt.Sprintf("Failed to get file from url: %s", *fileResult.Description))

					message = messageFailedToGetFile
				}
			} else {
				logError(fmt.Sprintf("Failed to get file id from shortened file id: `%s`, maybe bot was restarted?", shortenedFileID))

				message = messageFailedToGetFile
			}
		} else {
			logError(fmt.Sprintf("Failed to parse command: %s", data))

			message = messageUnprocessable
		}
	}

	// answer callback query
	if apiResult := b.AnswerCallbackQuery(query.ID, nil); apiResult.Ok {
		// edit message and remove inline keyboards
		if apiResult := b.EditMessageText(
			message,
			bot.OptionsEditMessageText{}.SetIDs(query.Message.Chat.ID, query.Message.MessageID),
		); apiResult.Ok {
			result = true
		} else {
			logError(fmt.Sprintf("Failed to edit message text: %s", *apiResult.Description))
		}
	} else {
		logError(fmt.Sprintf("Failed to answer callback query: %+v", query))
	}

	return result
}

// read bytes from given url
func readBytes(url string) (bytes []byte, err error) {
	var response *http.Response
	response, err = http.Get(url)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	bytes, err = ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func processImageForFaces(img image.Image, detected kakaoapi.ResponseDetectedFace, command VisionCommand) image.Image {
	var err error

	// image's width and height
	width, height := float64(detected.Result.Width), float64(detected.Result.Height)

	// copy to a new image
	newImg := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(newImg, newImg.Bounds(), img, image.ZP, draw.Src)
	gc := draw2dimg.NewGraphicContext(newImg)
	gc.SetLineWidth(StrokeWidth)
	gc.SetFillColor(color.Transparent)

	// build up facial attributes string
	for i, f := range detected.Result.Faces {
		switch command {
		case DetectFaces:
			// prepare freetype font
			fc := freetype.NewContext()
			fc.SetFont(font)
			fc.SetDPI(72)
			fc.SetClip(newImg.Bounds())
			fc.SetDst(newImg)
			fontSize := float64(newImg.Bounds().Dy()) / 24.0
			fc.SetFontSize(fontSize)

			// set color
			color := colorForIndex(i)
			gc.SetStrokeColor(color)
			fc.SetSrc(&image.Uniform{color})

			// draw rectangles and their indices on detected faces
			gc.MoveTo(width*f.X, height*f.Y)
			gc.LineTo(width*(f.X+f.W), height*f.Y)
			gc.LineTo(width*(f.X+f.W), height*(f.Y+f.H))
			gc.LineTo(width*f.X, height*(f.Y+f.H))
			gc.LineTo(width*f.X, height*f.Y)
			gc.Close()
			gc.FillStroke()

			// draw face label
			if _, err = fc.DrawString(
				fmt.Sprintf("Face #%d", i+1),
				freetype.Pt(
					int(width*f.X+5),
					int(fc.PointToFixed(height*(f.Y+f.H)-5)>>6),
				),
			); err != nil {
				logError(fmt.Sprintf("Failed to draw string: %s", err))
			}

			// mark nose
			nosePoints := f.FacialPoints.Nose
			for _, n := range nosePoints {
				gc.MoveTo(width*n.X(), height*n.Y())
				gc.ArcTo(width*n.X(), height*n.Y(), CircleRadius, CircleRadius, 0, -math.Pi*2)
				gc.Close()
				gc.FillStroke()
			}

			// mark right eye
			rightEyePoints := f.FacialPoints.RightEye
			for _, r := range rightEyePoints {
				gc.MoveTo(width*r.X(), height*r.Y())
				gc.ArcTo(width*r.X(), height*r.Y(), CircleRadius, CircleRadius, 0, -math.Pi*2)
				gc.Close()
				gc.FillStroke()
			}

			// mark left pupil
			leftEyePoints := f.FacialPoints.LeftEye
			for _, l := range leftEyePoints {
				gc.MoveTo(width*l.X(), height*l.Y())
				gc.ArcTo(width*l.X(), height*l.Y(), CircleRadius, CircleRadius, 0, -math.Pi*2)
				gc.Close()
				gc.FillStroke()
			}

			// mark lips
			lipPoints := f.FacialPoints.Lip
			for _, l := range lipPoints {
				gc.MoveTo(width*l.X(), height*l.Y())
				gc.ArcTo(width*l.X(), height*l.Y(), CircleRadius, CircleRadius, 0, -math.Pi*2)
				gc.Close()
				gc.FillStroke()
			}
		case MaskFaces:
			// pixelate face rects
			g := gift.New(
				gift.Pixelate(int(width * f.W / 8)),
			)
			g.DrawAt(
				newImg,
				newImg.SubImage(image.Rect(
					int(width*f.X),
					int(height*f.Y),
					int(width*(f.X+f.W)),
					int(height*(f.Y+f.H)),
				)),
				image.Pt(
					int(width*f.X),
					int(height*f.Y),
				),
				gift.CopyOperator,
			)
		}
	}
	gc.Save()

	return newImg
}

func processImageForProducts(img image.Image, detected kakaoapi.ResponseDetectedProduct) (image.Image, []string) {
	var err error

	// image's width and height
	width, height := float64(detected.Result.Width), float64(detected.Result.Height)

	newImg := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(newImg, newImg.Bounds(), img, image.ZP, draw.Src)
	gc := draw2dimg.NewGraphicContext(newImg)
	gc.SetLineWidth(StrokeWidth)
	gc.SetFillColor(color.Transparent)

	// build up facial attributes string
	classes := []string{}
	for i, o := range detected.Result.Objects {
		classes = append(classes, o.Class)

		// prepare freetype font
		fc := freetype.NewContext()
		fc.SetFont(font)
		fc.SetDPI(72)
		fc.SetClip(newImg.Bounds())
		fc.SetDst(newImg)
		fontSize := float64(newImg.Bounds().Dy()) / 24.0
		fc.SetFontSize(fontSize)

		// set color
		color := colorForIndex(i)
		gc.SetStrokeColor(color)
		fc.SetSrc(&image.Uniform{color})

		// draw rectangles and their indices on detected product
		gc.MoveTo(width*o.X1, height*o.Y1)
		gc.LineTo(width*o.X1, height*o.Y2)
		gc.LineTo(width*o.X2, height*o.Y2)
		gc.LineTo(width*o.X2, height*o.Y1)
		gc.LineTo(width*o.X1, height*o.Y1)
		gc.Close()
		gc.FillStroke()

		// draw product label
		if _, err = fc.DrawString(
			fmt.Sprintf("#%d: %s", i+1, o.Class),
			freetype.Pt(
				int(width*o.X1+5),
				int(fc.PointToFixed(height*o.Y2-5)>>6),
			),
		); err != nil {
			logError(fmt.Sprintf("Failed to draw string: %s", err))
		}
	}
	gc.Save()

	return newImg, classes
}

func processImageForPoses(img image.Image, analyzed kakaoapi.ResponseAnalyzedPose) image.Image {
	// copy to a new image
	newImg := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(newImg, newImg.Bounds(), img, image.ZP, draw.Src)
	gc := draw2dimg.NewGraphicContext(newImg)
	gc.SetLineWidth(StrokeWidth)
	gc.SetFillColor(color.Transparent)

	// draw lines on poses
	for i, pose := range analyzed {
		// set stroke color
		color := colorForIndex(i)
		gc.SetStrokeColor(color)

		// mark keypoints and connect them

		// nose
		noseX, noseY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexNose)
		gc.MoveTo(noseX, noseY)
		gc.ArcTo(noseX, noseY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left eye
		leftEyeX, leftEyeY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftEye)
		gc.MoveTo(leftEyeX, leftEyeY)
		gc.ArcTo(leftEyeX, leftEyeY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right eye
		rightEyeX, rightEyeY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightEye)
		gc.MoveTo(rightEyeX, rightEyeY)
		gc.ArcTo(rightEyeX, rightEyeY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left ear
		leftEarX, leftEarY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftEar)
		gc.MoveTo(leftEarX, leftEarY)
		gc.ArcTo(leftEarX, leftEarY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right ear
		rightEarX, rightEarY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightEar)
		gc.MoveTo(rightEarX, rightEarY)
		gc.ArcTo(rightEarX, rightEarY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left shoulder
		leftShoulderX, leftShoulderY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftShoulder)
		gc.MoveTo(leftShoulderX, leftShoulderY)
		gc.ArcTo(leftShoulderX, leftShoulderY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right shoulder
		rightShoulderX, rightShoulderY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightShoulder)
		gc.MoveTo(rightShoulderX, rightShoulderY)
		gc.ArcTo(rightShoulderX, rightShoulderY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left shoulder to right shoulder
		gc.MoveTo(leftShoulderX, leftShoulderY)
		gc.LineTo(rightShoulderX, rightShoulderY)
		gc.Close()
		gc.FillStroke()

		// left elbow
		leftElbowX, leftElbowY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftElbow)
		gc.MoveTo(leftElbowX, leftElbowY)
		gc.ArcTo(leftElbowX, leftElbowY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left shoulder to left elbow
		gc.MoveTo(leftShoulderX, leftShoulderY)
		gc.LineTo(leftElbowX, leftElbowY)
		gc.Close()
		gc.FillStroke()

		// right elbow
		rightElbowX, rightElbowY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightElbow)
		gc.MoveTo(rightElbowX, rightElbowY)
		gc.ArcTo(rightElbowX, rightElbowY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right shoulder to right elbow
		gc.MoveTo(rightShoulderX, rightShoulderY)
		gc.LineTo(rightElbowX, rightElbowY)
		gc.Close()
		gc.FillStroke()

		// left wrist
		leftWristX, leftWristY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftWrist)
		gc.MoveTo(leftWristX, leftWristY)
		gc.ArcTo(leftWristX, leftWristY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left elbow to left wrist
		gc.MoveTo(leftElbowX, leftElbowY)
		gc.LineTo(leftWristX, leftWristY)
		gc.Close()
		gc.FillStroke()

		// right wrist
		rightWristX, rightWristY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightWrist)
		gc.MoveTo(rightWristX, rightWristY)
		gc.ArcTo(rightWristX, rightWristY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right elbow to right wrist
		gc.MoveTo(rightElbowX, rightElbowY)
		gc.LineTo(rightWristX, rightWristY)
		gc.Close()
		gc.FillStroke()

		// left hip
		leftHipX, leftHipY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftHip)
		gc.MoveTo(leftHipX, leftHipY)
		gc.ArcTo(leftHipX, leftHipY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right hip
		rightHipX, rightHipY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightHip)
		gc.MoveTo(rightHipX, rightHipY)
		gc.ArcTo(rightHipX, rightHipY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left hip to right hip
		gc.MoveTo(leftHipX, leftHipY)
		gc.LineTo(rightHipX, rightHipY)
		gc.Close()
		gc.FillStroke()

		// left shoulder to right hip
		gc.MoveTo(leftShoulderX, leftShoulderY)
		gc.LineTo(rightHipX, rightHipY)
		gc.Close()
		gc.FillStroke()

		// right shoulder to left hip
		gc.MoveTo(rightShoulderX, rightShoulderY)
		gc.LineTo(leftHipX, leftHipY)
		gc.Close()
		gc.FillStroke()

		// left knee
		leftKneeX, leftKneeY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftKnee)
		gc.MoveTo(leftKneeX, leftKneeY)
		gc.ArcTo(leftKneeX, leftKneeY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left hip to left knee
		gc.MoveTo(leftHipX, leftHipY)
		gc.LineTo(leftKneeX, leftKneeY)
		gc.Close()
		gc.FillStroke()

		// right knee
		rightKneeX, rightKneeY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightKnee)
		gc.MoveTo(rightKneeX, rightKneeY)
		gc.ArcTo(rightKneeX, rightKneeY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right hip to right knee
		gc.MoveTo(rightHipX, rightHipY)
		gc.LineTo(rightKneeX, rightKneeY)
		gc.Close()
		gc.FillStroke()

		// left ankle
		leftAnkleX, leftAnkleY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexLeftAnkle)
		gc.MoveTo(leftAnkleX, leftAnkleY)
		gc.ArcTo(leftAnkleX, leftAnkleY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// left knee to left ankle
		gc.MoveTo(leftKneeX, leftKneeY)
		gc.LineTo(leftAnkleX, leftAnkleY)
		gc.Close()
		gc.FillStroke()

		// right ankle
		rightAnkleX, rightAnkleY, _ := pose.KeyPointFor(kakaoapi.KeyPointIndexRightAnkle)
		gc.MoveTo(rightAnkleX, rightAnkleY)
		gc.ArcTo(rightAnkleX, rightAnkleY, PosePointRadius, PosePointRadius, 0, -math.Pi*2)
		gc.Close()
		gc.FillStroke()

		// right knee to right ankle
		gc.MoveTo(rightKneeX, rightKneeY)
		gc.LineTo(rightAnkleX, rightAnkleY)
		gc.Close()
		gc.FillStroke()
	}

	gc.Save()

	return newImg
}

// process requested image processing
func processImage(b *bot.Bot, chatID int64, messageIDToDelete int, fileURL string, command VisionCommand) {
	errorMessage := ""

	// 'typing...'
	b.SendChatAction(chatID, bot.ChatActionTyping)

	var imgBytes []byte
	var err error

	// read image file from url
	if imgBytes, err = readBytes(fileURL); err == nil {
		switch command {
		case DetectFaces, MaskFaces:
			var detected kakaoapi.ResponseDetectedFace
			detected, err = kakaoClient.DetectFaceFromBytes(imgBytes, 0.7)
			if err == nil {
				if len(detected.Result.Faces) > 0 {
					var img image.Image
					imgReader := bytes.NewReader(imgBytes)
					img, _, err = image.Decode(imgReader)
					if err == nil {
						// process image
						newImg := processImageForFaces(img, detected, command)

						// 'uploading photo...'
						b.SendChatAction(chatID, bot.ChatActionUploadPhoto)

						// send a photo with rectangles drawn on detected faces
						buf := new(bytes.Buffer)
						err = jpeg.Encode(buf, newImg, nil)
						if err == nil {
							if sent := b.SendPhoto(
								chatID,
								bot.InputFileFromBytes(buf.Bytes()),
								bot.OptionsSendPhoto{}.SetCaption(fmt.Sprintf("Process result of '%s'", command)),
							); !sent.Ok {
								errorMessage = fmt.Sprintf("Failed to send image: %s", *sent.Description)
							}
						} else {
							errorMessage = fmt.Sprintf("Failed to encode image: %s", err)
						}
					} else {
						errorMessage = fmt.Sprintf("Failed to decode image: %s", err)
					}
				} else {
					errorMessage = "No face detected on this image."
				}
			} else {
				errorMessage = fmt.Sprintf("Failed to detect faces: %s", err)
			}
		case DetectProducts:
			var detected kakaoapi.ResponseDetectedProduct
			detected, err = kakaoClient.DetectProductFromBytes(imgBytes, 0.7)
			if err == nil {
				if len(detected.Result.Objects) > 0 {
					var img image.Image
					imgReader := bytes.NewReader(imgBytes)
					img, _, err = image.Decode(imgReader)
					if err == nil {
						newImg, classes := processImageForProducts(img, detected)

						// 'uploading photo...'
						b.SendChatAction(chatID, bot.ChatActionUploadPhoto)

						// send a photo with rectangles drawn on detected faces
						buf := new(bytes.Buffer)
						err = jpeg.Encode(buf, newImg, nil)
						if err == nil {
							if sent := b.SendPhoto(
								chatID,
								bot.InputFileFromBytes(buf.Bytes()),
								bot.OptionsSendPhoto{}.SetCaption(fmt.Sprintf("Process result of '%s':\n\n%s", command, strings.Join(classes, "\n"))),
							); !sent.Ok {
								errorMessage = fmt.Sprintf("Failed to send image: %s", *sent.Description)
							}
						} else {
							errorMessage = fmt.Sprintf("Failed to encode image: %s", err)
						}
					} else {
						errorMessage = fmt.Sprintf("Failed to decode image: %s", err)
					}
				} else {
					errorMessage = "No product detected on this image."
				}
			}
		case DetectNSFW:
			if detected, err := kakaoClient.DetectNSFWFromBytes(imgBytes); err == nil {
				// send nsfw factors
				message := fmt.Sprintf(`Process result of '%s':

Normal: %.2f%%
Soft: %.2f%%
Adult: %.2f%%`,
					command,
					100.0*detected.Result.Normal,
					100.0*detected.Result.Soft,
					100.0*detected.Result.Adult,
				)
				if sent := b.SendMessage(chatID, message, nil); !sent.Ok {
					errorMessage = fmt.Sprintf("Failed to send nsfw factors: %s", *sent.Description)
				}
			} else {
				errorMessage = fmt.Sprintf("Failed to detect NSFW factors from image: %s", err)
			}
		case Tag:
			if generated, err := kakaoClient.GenerateTagsFromBytes(imgBytes); err == nil {
				if len(generated.Result.Labels) > 0 {
					tags := []string{}
					for i := 0; i < len(generated.Result.Labels); i++ {
						tags = append(tags, fmt.Sprintf("%s (%s)", generated.Result.Labels[i], generated.Result.LabelsKorean[i]))
					}

					// send tags
					message := fmt.Sprintf("Process result of '%s':\n\n%s", command, strings.Join(tags, "\n"))
					if sent := b.SendMessage(chatID, message, nil); !sent.Ok {
						errorMessage = fmt.Sprintf("Failed to send tags: %s", *sent.Description)
					}
				} else {
					errorMessage = "Could not tag given image."
				}
			} else {
				errorMessage = fmt.Sprintf("Failed to tag image: %s", err)
			}
		case AnalyzePoses:
			var analyzed kakaoapi.ResponseAnalyzedPose
			analyzed, err = kakaoClient.AnalyzePoseFromBytes(imgBytes)
			if err == nil {
				var img image.Image
				imgReader := bytes.NewReader(imgBytes)
				img, _, err = image.Decode(imgReader)
				if err == nil {
					newImg := processImageForPoses(img, analyzed)

					// 'uploading photo...'
					b.SendChatAction(chatID, bot.ChatActionUploadPhoto)

					// send a photo with lines drawn on poses
					buf := new(bytes.Buffer)
					err = jpeg.Encode(buf, newImg, nil)
					if err == nil {
						if sent := b.SendPhoto(
							chatID,
							bot.InputFileFromBytes(buf.Bytes()),
							bot.OptionsSendPhoto{}.SetCaption(fmt.Sprintf("Process result of '%s'", command)),
						); !sent.Ok {
							errorMessage = fmt.Sprintf("Failed to send image: %s", *sent.Description)
						}
					} else {
						errorMessage = fmt.Sprintf("Failed to encode image: %s", err)
					}
				} else {
					errorMessage = fmt.Sprintf("Failed to decode image: %s", err)
				}
			} else {
				errorMessage = fmt.Sprintf("Failed to detect faces: %s", err)
			}
		case ExtractTexts:
			var detected kakaoapi.ResponseDetectedText
			detected, err = kakaoClient.DetectTextFromBytes(imgBytes)
			if err == nil {
				strs := []string{}
				for _, result := range detected.Result {
					strs = append(strs, result.RecognizedWords...)
				}

				message := fmt.Sprintf(`Process result of '%s':

%s`,
					command,
					strings.Join(strs, ", "),
				)
				if sent := b.SendMessage(chatID, message, nil); !sent.Ok {
					errorMessage = fmt.Sprintf("Failed to send extracted texts: %s", *sent.Description)
				}
			} else {
				errorMessage = fmt.Sprintf("Failed to detect texts: %s", err)
			}
		default:
			errorMessage = fmt.Sprintf("Command not supported: %s", command)
		}
	} else {
		errorMessage = fmt.Sprintf("Failed to read file from %s: %s", fileURL, err)
	}

	// delete original message
	b.DeleteMessage(chatID, messageIDToDelete)

	// if there was any error, send it back
	if errorMessage != "" {
		b.SendMessage(chatID, errorMessage, nil)

		logError(errorMessage)
	}
}

// generate inline keyboards for selecting action
func genImageInlineKeyboards(fileID string) [][]bot.InlineKeyboardButton {
	shortenedFileID := fileID[:32]
	fileIDs[shortenedFileID] = fileID

	data := map[string]string{}
	for title, cmd := range allCmds {
		data[string(title)] = fmt.Sprintf("%s/%s", cmd, shortenedFileID)
	}

	cancel := commandCancel
	return append(bot.NewInlineKeyboardButtonsAsRowsWithCallbackData(data), []bot.InlineKeyboardButton{
		bot.InlineKeyboardButton{Text: strings.Title(commandCancel), CallbackData: &cancel},
	})
}

// rotate color
func colorForIndex(i int) color.RGBA {
	length := len(colors)
	return colors[i%length]
}
