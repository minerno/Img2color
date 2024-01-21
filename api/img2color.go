package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/fogleman/gg"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
)

type Response struct {
	RGB string `json:"RGB"`
}

var ctx = context.Background()
var rdb *redis.Client
var kvEnable bool
var kvURL string
var kvToken string

// init
/*
 * - Load .env
 * - Check Redis enable
 * - Check KV enable
 * - Start server
 */
func init() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file")
	}

	redisEnable, _ := strconv.ParseBool(os.Getenv("REDIS_ENABLE"))
	if redisEnable {
		rdb = redis.NewClient(&redis.Options{
			Addr:     os.Getenv("REDIS_HOST"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       0,
		})
	}

	kvEnable, _ = strconv.ParseBool(os.Getenv("KV_ENABLE"))
	if kvEnable {
		kvURL = os.Getenv("KV_REST_API_URL")
		kvToken = os.Getenv("KV_REST_API_TOKEN")
	}
}

// Handler
/**
 * @param w http.ResponseWriter
 * @param r *http.Request
 * @return void
 */
func Handler(w http.ResponseWriter, r *http.Request) {
	if !checkReferer(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	imgURL := r.URL.Query().Get("img")
	if imgURL == "" {
		http.Error(w, "img parameter is required", http.StatusBadRequest)
		return
	}

	var color string
	var err error
	if rdb != nil {
		color, err = getColorFromCache(imgURL)
		if err == redis.Nil {
			color, err = getColorFromImageURL(imgURL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			setColorToCache(imgURL, color)
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if kvEnable {
		color, err = getColorFromKV(imgURL)
		if err != nil {
			color, err = getColorFromImageURL(imgURL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			setColorToKV(imgURL, color)
		}
	} else {
		color, err = getColorFromImageURL(imgURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	res := Response{RGB: color}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// checkReferer
/**
 * @param r *http.Request
 * @return bool
 */
func checkReferer(r *http.Request) bool {
	referer := r.Header.Get("Referer")
	allowedReferers := strings.Split(os.Getenv("ALLOWED_REFERERS"), ",")
	for _, allowedReferer := range allowedReferers {
		if allowedReferer == "*" || strings.HasSuffix(referer, allowedReferer) {
			return true
		}
	}
	return false
}

// getColorFromCache
/**
 * @param imgURL string
 * @return string
 * @return error
 */
func getColorFromCache(imgURL string) (string, error) {
	return rdb.Get(ctx, imgURL).Result()
}

// setColorToCache
/**
 * @param imgURL string
 * @param color string
 * @return void
 */
func setColorToCache(imgURL string, color string) {
	rdb.Set(ctx, imgURL, color, 24*time.Hour)
}

// getColorFromKV
/**
 * @param imgURL string
 * @return string
 * @return error
 */
func getColorFromKV(imgURL string) (string, error) {
	resp, err := http.Get(kvURL + "/" + imgURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("Not Found")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// setColorToKV
/**
 * @param imgURL string
 * @param color string
 * @return void
 */
func setColorToKV(imgURL string, color string) {
	req, err := http.NewRequest("PUT", kvURL+"/"+imgURL, bytes.NewBuffer([]byte(color)))
	if err != nil {
		fmt.Println(err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+kvToken)
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
}

// getColorFromImageURL
/**
 * @param imgURL string
 * @return string
 * @return error
 */
func getColorFromImageURL(imgURL string) (string, error) {
	resp, err := http.Get(imgURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return "", err
	}

	img = imaging.Resize(img, 1, 1, imaging.Box)
	dc := gg.NewContext(1, 1)
	dc.DrawImage(img, 0, 0)
	rVal, g, b, _ := dc.Image().At(0, 0).RGBA()
	color := fmt.Sprintf("#%02X%02X%02X", uint8(rVal>>8), uint8(g>>8), uint8(b>>8))

	return color, nil
}