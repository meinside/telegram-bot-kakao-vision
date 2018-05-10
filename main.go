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

// LogglyLog struct
type LogglyLog struct {
	Application string      `json:"app"`
	Severity    string      `json:"severity"`
	Message     string      `json:"message,omitempty"`
	Object      interface{} `json:"obj,omitempty"`
}

// VisionCommand type
type VisionCommand string

// XXX - First letter of commands should be unique.
const (
	Face    VisionCommand = "Face Detection"
	Product VisionCommand = "Product Detection"
	NSFW    VisionCommand = "NSFW Detection"
	Tag     VisionCommand = "Tag This Image"

	// fun commands
	MaskFaces VisionCommand = "Mask Faces"
)

// XXX - When a new command is added, add it here too.
var allCmds = []VisionCommand{
	Face,
	Product,
	NSFW,
	Tag,

	// fun commands
	MaskFaces,
}
var shortCmdsMap = map[VisionCommand]string{}
var cmdsMap = map[string]VisionCommand{}

var kakaoClient *kakaoapi.Client

var font *truetype.Font

const (
	messageActionImage     = "Choose action for this image:"
	messageUnprocessable   = "Unprocessable message."
	messageFailedToGetFile = "Failed to get file from the server."
	messageCanceled        = "Canceled."
	messageHelp            = `Send any image to this bot, then select one of the following actions:

- Face Detection
- Product Detection
- NSFW Detection
- Tag This Image
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

func init() {
	// read from config file
	if file, err := ioutil.ReadFile(configFilename); err != nil {
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

	// commands
	var firstLetter string
	for _, c := range allCmds {
		firstLetter = string(string(c)[0])

		shortCmdsMap[c] = firstLetter
		cmdsMap[firstLetter] = c
	}

	// telegram bot client
	client = bot.NewClient(conf.TelegramAPIToken)
	client.Verbose = conf.IsVerbose

	// loggly logger client
	if conf.LogglyToken != "" {
		logger = loggly.New(conf.LogglyToken)
	}

	// others
	if bytes, err := ioutil.ReadFile(fontFilepath); err == nil {
		if f, err := truetype.Parse(bytes); err == nil {
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
		logger.Log(LogglyLog{
			Application: appName,
			Severity:    "Log",
			Message:     message,
		})
	}
}

// log error message
func logError(message string) {
	log.Println(message)

	if logger != nil {
		logger.Log(LogglyLog{
			Application: appName,
			Severity:    "Error",
			Message:     message,
		})
	}
}

// log request from user
func logRequest(username, fileURL string, command VisionCommand) {
	if logger != nil {
		logger.Log(LogglyLog{
			Application: appName,
			Severity:    "Verbose",
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
	var options = map[string]interface{}{
		"reply_to_message_id": update.Message.MessageID,
	}

	if update.Message.HasPhoto() {
		lastIndex := len(update.Message.Photo) - 1 // XXX - last one is the largest

		options["reply_markup"] = bot.InlineKeyboardMarkup{
			InlineKeyboard: genImageInlineKeyboards(update.Message.Photo[lastIndex].FileID),
		}
		message = messageActionImage
	} else if update.Message.HasDocument() && strings.HasPrefix(*update.Message.Document.MimeType, "image/") {
		options["reply_markup"] = bot.InlineKeyboardMarkup{
			InlineKeyboard: genImageInlineKeyboards(update.Message.Document.FileID),
		}
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
func processCallbackQuery(b *bot.Bot, update bot.Update) bool {
	// process result
	result := false

	var username string
	message := ""
	query := *update.CallbackQuery
	data := *query.Data

	if data == commandCancel {
		message = messageCanceled
	} else {
		command := cmdsMap[string(data[0])]
		fileID := string(data[1:])

		if fileResult := b.GetFile(fileID); fileResult.Ok {
			fileURL := b.GetFileURL(*fileResult.Result)

			if strings.Contains(*query.Message.Text, "image") {
				go processImage(b, query.Message.Chat.ID, query.Message.MessageID, fileURL, command)

				message = fmt.Sprintf("Processing '%s' on received image...", command)

				// log request
				if query.From.Username == nil {
					username = query.From.FirstName
				} else {
					username = *query.From.Username
				}
				logRequest(username, fileURL, command)
			} else {
				message = messageUnprocessable
			}
		} else {
			logError(fmt.Sprintf("Failed to get file from url: %s", *fileResult.Description))

			message = messageFailedToGetFile
		}
	}

	// answer callback query
	if apiResult := b.AnswerCallbackQuery(query.ID, nil); apiResult.Ok {
		// edit message and remove inline keyboards
		options := map[string]interface{}{
			"chat_id":    query.Message.Chat.ID,
			"message_id": query.Message.MessageID,
		}
		if apiResult := b.EditMessageText(message, options); apiResult.Ok {
			result = true
		} else {
			logError(fmt.Sprintf("Failed to edit message text: %s", *apiResult.Description))
		}
	} else {
		logError(fmt.Sprintf("Failed to answer callback query: %+v", query))
	}

	return result
}

// process requested image processing
func processImage(b *bot.Bot, chatID int64, messageIDToDelete int, fileURL string, command VisionCommand) {
	errorMessage := ""

	// 'typing...'
	b.SendChatAction(chatID, bot.ChatActionTyping)

	switch command {
	case Face, MaskFaces:
		if detected, err := kakaoClient.DetectFaceFromURL(fileURL, 0.7); err == nil {
			if len(detected.Result.Faces) > 0 {
				// open image from url,
				if resp, err := http.Get(fileURL); err == nil {
					defer resp.Body.Close()

					// image's width and height
					width, height := float64(detected.Result.Width), float64(detected.Result.Height)

					if img, _, err := image.Decode(resp.Body); err == nil {
						// copy to a new image
						newImg := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
						draw.Draw(newImg, newImg.Bounds(), img, image.ZP, draw.Src)
						gc := draw2dimg.NewGraphicContext(newImg)
						gc.SetLineWidth(StrokeWidth)
						gc.SetFillColor(color.Transparent)

						// build up facial attributes string
						for i, f := range detected.Result.Faces {
							switch command {
							case Face:
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

						// 'uploading photo...'
						b.SendChatAction(chatID, bot.ChatActionUploadPhoto)

						// send a photo with rectangles drawn on detected faces
						buf := new(bytes.Buffer)
						if err := jpeg.Encode(buf, newImg, nil); err == nil {
							if sent := b.SendPhoto(chatID, bot.InputFileFromBytes(buf.Bytes()), map[string]interface{}{
								"caption": fmt.Sprintf("Process result of '%s'", command),
							}); !sent.Ok {
								errorMessage = fmt.Sprintf("Failed to send image: %s", *sent.Description)
							}
						} else {
							errorMessage = fmt.Sprintf("Failed to encode image: %s", err)
						}
					} else {
						errorMessage = fmt.Sprintf("Failed to decode image: %s", err)
					}
				} else {
					errorMessage = fmt.Sprintf("Failed to open image: %s", err)
				}
			} else {
				errorMessage = "No face detected on this image."
			}
		} else {
			errorMessage = fmt.Sprintf("Failed to detect faces: %s", err)
		}
	case Product:
		if detected, err := kakaoClient.DetectProductFromURL(fileURL, 0.7); err == nil {
			if len(detected.Result.Objects) > 0 {
				// open image from url,
				if resp, err := http.Get(fileURL); err == nil {
					defer resp.Body.Close()

					// image's width and height
					width, height := float64(detected.Result.Width), float64(detected.Result.Height)

					if img, _, err := image.Decode(resp.Body); err == nil {
						// copy to a new image
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

						// 'uploading photo...'
						b.SendChatAction(chatID, bot.ChatActionUploadPhoto)

						// send a photo with rectangles drawn on detected faces
						buf := new(bytes.Buffer)
						if err := jpeg.Encode(buf, newImg, nil); err == nil {
							if sent := b.SendPhoto(chatID, bot.InputFileFromBytes(buf.Bytes()), map[string]interface{}{
								"caption": fmt.Sprintf("Process result of '%s':\n\n%s", command, strings.Join(classes, "\n")),
							}); !sent.Ok {
								errorMessage = fmt.Sprintf("Failed to send image: %s", *sent.Description)
							}
						} else {
							errorMessage = fmt.Sprintf("Failed to encode image: %s", err)
						}
					} else {
						errorMessage = fmt.Sprintf("Failed to decode image: %s", err)
					}
				} else {
					errorMessage = fmt.Sprintf("Failed to open image: %s", err)
				}
			} else {
				errorMessage = "No product detected on this image."
			}
		}
	case NSFW:
		if detected, err := kakaoClient.DetectNSFWFromURL(fileURL); err == nil {
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
		if generated, err := kakaoClient.GenerateTagsFromURL(fileURL); err == nil {
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
	default:
		errorMessage = fmt.Sprintf("Command not supported: %s", command)
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
	data := map[string]string{}
	for _, cmd := range allCmds {
		data[string(cmd)] = fmt.Sprintf("%s%s", shortCmdsMap[cmd], fileID)
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
