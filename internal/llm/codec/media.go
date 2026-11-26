package codec

import (
	"encoding/base64"
	"errors"
	"net/url"
	"path"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func DecodeGeminiMedia(entry map[string]any) (*model.Media, error) {
	media := &model.Media{}
	switch {
	case entry["inlineData"] != nil:
		inline := model.Object(entry["inlineData"])
		media.Data, media.MIMEType = model.String(inline["data"]), model.String(inline["mimeType"])
	case entry["fileData"] != nil:
		file := model.Object(entry["fileData"])
		media.URL, media.MIMEType = model.String(file["fileUri"]), model.String(file["mimeType"])
	default:
		return nil, nil
	}
	media.Kind = mediaKind(media.MIMEType)
	return media, validateMedia(media)
}

func decodeMedia(entry map[string]any) (*model.Media, error) {
	media := &model.Media{}
	switch blockType := model.String(entry["type"]); blockType {
	case "image_url":
		image := model.Object(entry["image_url"])
		media.Kind, media.URL, media.Detail = "image", model.String(image["url"]), model.String(image["detail"])
	case "input_image":
		media.Kind, media.URL, media.FileID, media.Detail = "image", model.String(entry["image_url"]), model.String(entry["file_id"]), model.String(entry["detail"])
	case "image", "document":
		source := model.Object(entry["source"])
		switch model.String(source["type"]) {
		case "base64", "url":
			media.URL, media.Data, media.MIMEType = model.String(source["url"]), model.String(source["data"]), model.String(source["media_type"])
		case "file":
			media.FileID = model.String(source["file_id"])
		default:
			return nil, nil
		}
		media.Kind = blockType
	case "input_audio":
		audio := model.Object(entry["input_audio"])
		if audio == nil {
			audio = entry
		}
		media.Kind, media.Data = "audio", model.String(audio["data"])
		media.MIMEType = map[string]string{"wav": "audio/wav", "mp3": "audio/mpeg"}[model.String(audio["format"])]
		if media.MIMEType == "" {
			return nil, errors.New("unsupported input audio format")
		}
	case "file", "input_file":
		file := entry
		if blockType == "file" {
			file = model.Object(entry["file"])
		}
		media.Kind, media.FileID, media.Filename, media.URL, media.Data = "document", model.String(file["file_id"]), model.String(file["filename"]), model.String(file["file_url"]), model.String(file["file_data"])
	default:
		return nil, nil
	}
	return media, validateMedia(media)
}

func validateMedia(media *model.Media) error {
	if strings.HasPrefix(media.URL, "data:") {
		media.Data, media.URL = media.URL, ""
	}
	if strings.HasPrefix(media.Data, "data:") {
		header, payload, valid := strings.Cut(media.Data, ",")
		if !valid || !strings.HasSuffix(header, ";base64") {
			return errors.New("media data URL must use base64")
		}
		media.MIMEType, media.Data = strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), payload
	}
	if media.MIMEType == "" {
		extension := ""
		if media.URL != "" {
			address, err := url.Parse(media.URL)
			if err != nil {
				return errors.New("invalid media URL")
			}
			extension = strings.ToLower(path.Ext(address.Path))
		}
		if extension == "" {
			extension = strings.ToLower(path.Ext(media.Filename))
		}
		media.MIMEType = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif", ".pdf": "application/pdf", ".wav": "audio/wav", ".mp3": "audio/mpeg"}[extension]
	}
	if media.Data != "" {
		if _, err := base64.StdEncoding.DecodeString(media.Data); err != nil {
			if _, rawErr := base64.RawStdEncoding.DecodeString(media.Data); rawErr != nil {
				return errors.New("invalid base64 media payload")
			}
		}
	}
	count := 0
	for _, value := range []string{media.URL, media.Data, media.FileID} {
		if value != "" {
			count++
		}
	}
	if count != 1 {
		return errors.New("media must have exactly one URL, data payload or file identifier")
	}
	return nil
}

func mediaKind(mime string) string {
	for _, kind := range []string{"image", "audio", "video"} {
		if strings.HasPrefix(mime, kind+"/") {
			return kind
		}
	}
	return "document"
}

func EncodeMedia(media *model.Media, target model.Format) (map[string]any, error) {
	if media == nil {
		return nil, errors.New("media part has no media")
	}
	if media.FileID != "" {
		return encodeFileMedia(media, target)
	}
	address := media.URL
	if media.Data != "" {
		mime := media.MIMEType
		if mime == "" {
			mime = "application/octet-stream"
		}
		address = "data:" + mime + ";base64," + media.Data
	}
	switch target {
	case model.Chat:
		return encodeChatMedia(media, address)
	case model.Responses:
		switch media.Kind {
		case "image":
			part := map[string]any{"type": "input_image", "image_url": address}
			if media.Detail != "" {
				part["detail"] = media.Detail
			}
			return part, nil
		case "document":
			part := map[string]any{"type": "input_file"}
			if media.Data != "" {
				part["file_data"] = address
			} else {
				part["file_url"] = address
			}
			if media.Filename != "" {
				part["filename"] = media.Filename
			}
			return part, nil
		}
	case model.Messages:
		if media.Kind == "image" || media.Kind == "document" {
			if media.Kind == "document" && media.MIMEType != "application/pdf" && media.MIMEType != "text/plain" {
				return nil, errors.New("messages document conversion requires PDF or plain text")
			}
			source := map[string]any{"type": "url", "url": address}
			if media.Data != "" {
				source = map[string]any{"type": "base64", "media_type": media.MIMEType, "data": media.Data}
			}
			return map[string]any{"type": media.Kind, "source": source}, nil
		}
	case model.Gemini:
		if media.MIMEType == "" {
			return nil, errors.New("gemini media requires a known MIME type")
		}
		if media.Data != "" {
			return map[string]any{"inlineData": map[string]any{"mimeType": media.MIMEType, "data": media.Data}}, nil
		}
		return map[string]any{"fileData": map[string]any{"mimeType": media.MIMEType, "fileUri": media.URL}}, nil
	}
	return nil, errors.New("target protocol cannot represent this media content")
}

func encodeFileMedia(media *model.Media, target model.Format) (map[string]any, error) {
	switch {
	case target == model.Responses && media.Kind == "image":
		return map[string]any{"type": "input_image", "file_id": media.FileID}, nil
	case target == model.Responses && media.Kind == "document":
		return map[string]any{"type": "input_file", "file_id": media.FileID}, nil
	case target == model.Messages && (media.Kind == "image" || media.Kind == "document"):
		return map[string]any{"type": media.Kind, "source": map[string]any{"type": "file", "file_id": media.FileID}}, nil
	case target == model.Chat && media.Kind == "document":
		return map[string]any{"type": "file", "file": map[string]any{"file_id": media.FileID}}, nil
	}
	return nil, errors.New("provider file identifiers cannot cross protocols")
}

func encodeChatMedia(media *model.Media, address string) (map[string]any, error) {
	switch media.Kind {
	case "image":
		image := map[string]any{"url": address}
		if media.Detail != "" {
			image["detail"] = media.Detail
		}
		return map[string]any{"type": "image_url", "image_url": image}, nil
	case "document":
		if media.Data == "" {
			return nil, errors.New("chat file input requires inline data")
		}
		file := map[string]any{"file_data": address}
		if media.Filename != "" {
			file["filename"] = media.Filename
		}
		return map[string]any{"type": "file", "file": file}, nil
	case "audio":
		audioFormat := map[string]string{"audio/wav": "wav", "audio/mpeg": "mp3", "audio/mp3": "mp3"}[media.MIMEType]
		if media.Data == "" || audioFormat == "" {
			return nil, errors.New("chat audio requires inline WAV or MP3")
		}
		return map[string]any{"type": "input_audio", "input_audio": map[string]any{"format": audioFormat, "data": media.Data}}, nil
	}
	return nil, errors.New("target protocol cannot represent this media content")
}
