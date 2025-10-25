package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	config "github.com/phillip/contribution-tracker-go/config"
	models "github.com/phillip/contribution-tracker-go/models"
	utils "github.com/phillip/contribution-tracker-go/utils"
)

// ---------------- CREATE ----------------
func CreateHub(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Authenticated user ---
		uid := c.GetString("user_id")
		userID, err := primitive.ObjectIDFromHex(uid)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		// --- Bind form fields ---
		var input struct {
			Title        string   `form:"title" binding:"required"`
			Description  string   `form:"description"`
			Lat          float64  `form:"lat"`
			Lng          float64  `form:"lng"`
			LocationName string   `form:"location_name"`
			Rating       float64  `form:"rating"`
		}

		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}


		// --- Handle file uploads ---
		form, err := c.MultipartForm()
		if err != nil && err != http.ErrNotMultipart {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid form data"})
			return
		}

		var imageURLs []string
		if form != nil {
			files := form.File["images"] // key must be "images"
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open file"})
					return
				}

				url, err := utils.UploadToCloudinary(file, fileHeader)
				file.Close()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error":   "image upload failed",
						"details": err.Error(),
						"file":    fileHeader.Filename,
					})
					return
				}

				imageURLs = append(imageURLs, url)
			}
		}

		// --- Save hub ---
		now := time.Now()
		hub := models.Hub{
			ID:           primitive.NewObjectID(),
			UserID:       userID,
			Title:        input.Title,
			Description:  input.Description,
			Coordinates: models.Coordinates{
				Lat: input.Lat,
				Lng: input.Lng,
			},
			LocationName: input.LocationName,
			Rating:       input.Rating,
			Images:       imageURLs,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		col := cfg.MongoClient.Database(cfg.DBName).Collection("hubs")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := col.InsertOne(ctx, hub); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create hub"})
			return
		}

		c.JSON(http.StatusCreated, hub)
	}
}


// ---------------- LIST ----------------
func ListHubs(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Validate user ID ---
		uid := c.GetString("user_id")
		userID, err := primitive.ObjectIDFromHex(uid)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		col := cfg.MongoClient.Database(cfg.DBName).Collection("hubs")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// --- Build filter ---
		filter := bson.M{"user_id": userID}
		if q := c.Query("q"); q != "" {
			filter["title"] = bson.M{"$regex": q, "$options": "i"}
		}

		// --- Fetch data ---
		cursor, err := col.Find(ctx, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch hubs"})
			return
		}

		var hubs []models.Hub
		if err := cursor.All(ctx, &hubs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not decode hubs"})
			return
		}

		if len(hubs) == 0 {
			c.JSON(http.StatusOK, []models.Hub{})
			return
		}

		// --- Pick the most recently updated hub ---
		latest := hubs[0]
		for _, ev := range hubs {
			if ev.UpdatedAt.After(latest.UpdatedAt) {
				latest = ev
			}
		}

		// --- Generate ETag from latest hub ---
		etag := utils.GenerateETag(latest.ID, latest.UpdatedAt)
		if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
			c.Status(http.StatusNotModified)
			return
		}
		c.Header("ETag", etag)

		// --- Add Last-Modified from latest hub ---
		c.Header("Last-Modified", latest.UpdatedAt.UTC().Format(http.TimeFormat))

		c.JSON(http.StatusOK, hubs)
	}
}

// ---------------- GET ----------------
func GetHub(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString("user_id")
		userID, err := primitive.ObjectIDFromHex(uid)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		hubID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		var hub models.Hub
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = cfg.MongoClient.Database(cfg.DBName).
			Collection("hubs").
			FindOne(ctx, bson.M{"_id": hubID, "user_id": userID}).
			Decode(&hub)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Hub not found or not owned"})
			return
		}

		etag := utils.GenerateETag(hub.ID, hub.UpdatedAt)
		if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
			c.Status(http.StatusNotModified)
			return
		}
		c.Header("ETag", etag)

		c.JSON(http.StatusOK, hub)
	}
}

// ---------------- UPDATE ----------------
func UpdateHub(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ✅ Validate requester identity
		role := c.GetString("role")
		requesterID := c.GetString("user_id")
		if requesterID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// ✅ Validate hub ID
		id := c.Param("id")
		objID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid hub ID"})
			return
		}

		// ✅ Fetch existing hub
		col := cfg.MongoClient.Database(cfg.DBName).Collection("hubs")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var existing models.Hub
		if err := col.FindOne(ctx, bson.M{"_id": objID}).Decode(&existing); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Hub not found"})
			return
		}

		// ✅ Check permission
		if role != "admin" && existing.UserID.Hex() != requesterID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}

		// ✅ Bind input (form-data for mixed text + file upload)
		var input struct {
			Title        string   `form:"title" binding:"required"`
			Description  string   `form:"description"`
			Lat          *float64  `form:"lat"`
			Lng          *float64  `form:"lng"`
			LocationName string   `form:"location_name"`
			Rating       float64  `form:"rating"`
			Images       []string `form:"images"` 
		}


		if err := c.ShouldBind(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// ✅ Prepare update document
		update := bson.M{"updated_at": time.Now()}

		if input.Title != "" {
			update["title"] = input.Title
		}
		if input.Description != "" {
			update["description"] = input.Description
		}
		if input.LocationName != "" {
			update["location_name"] = input.LocationName
		}
		if input.Rating > 0 {
			update["rating"] = input.Rating
		}
		// Coordinates (nested)
		coordinatesUpdate := bson.M{}
		if input.Lat != nil { // assuming you use *float64 for optional numbers
			coordinatesUpdate["lat"] = input.Lat
		}
		if input.Lng != nil {
			coordinatesUpdate["lng"] = input.Lng
		}
		if len(coordinatesUpdate) > 0 {
			update["coordinates"] = coordinatesUpdate
		}

		// ✅ Handle new image uploads (multipart form)
		newImageURLs := []string{}
		form, _ := c.MultipartForm()
		if form != nil {
			files := form.File["new_images"] // key = "new_images"
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open image"})
					return
				}
				url, err := utils.UploadToCloudinary(file, fileHeader)
				file.Close()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "image upload failed", "details": err.Error()})
					return
				}
				newImageURLs = append(newImageURLs, url)
			}
		}

		// ✅ Merge images (keep provided + add new)
		if input.Images != nil || len(newImageURLs) > 0 {
			update["images"] = append(input.Images, newImageURLs...)
		}

		// ❗ Reject empty update
		if len(update) == 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
			return
		}

		// ✅ Apply update
		_, err = col.UpdateOne(ctx, bson.M{"_id": objID}, bson.M{"$set": update})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update event"})
			return
		}

		// ✅ Fetch updated hub
		var updated models.Hub
		if err := col.FindOne(ctx, bson.M{"_id": objID}).Decode(&updated); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve updated hub"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Hub updated successfully",
			"event":   updated,
		})
	}
}


// ---------------- DELETE ----------------
func DeleteHub(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ✅ Validate requester identity
		role := c.GetString("role")
		requesterID := c.GetString("user_id")
		if requesterID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// ✅ Validate hub ID
		oid, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		col := cfg.MongoClient.Database(cfg.DBName).Collection("hubs")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// ✅ Fetch existing hub
		var existing models.Hub
		if err := col.FindOne(ctx, bson.M{"_id": oid}).Decode(&existing); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Hub not found"})
			return
		}

		// ✅ Check permission
		if role != "admin" && existing.UserID.Hex() != requesterID {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}

		// ✅ Delete event
		res, err := col.DeleteOne(ctx, bson.M{"_id": oid})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete hub"})
			return
		}
		if res.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Hub not found"})
			return
		}

		// 🔹 (Optional) TODO: Delete images from Cloudinary
		for _, img := range existing.Images {
			  utils.DeleteFromCloudinary(img)
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "event deleted successfully",
			"id":      oid.Hex(),
		})
	}
}

