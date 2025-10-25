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
		uid := c.GetString("user_id")
		userID, err := primitive.ObjectIDFromHex(uid)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		hubCol := cfg.MongoClient.Database(cfg.DBName).Collection("hubs")
		reviewCol := cfg.MongoClient.Database(cfg.DBName).Collection("reviews")
		userCol := cfg.MongoClient.Database(cfg.DBName).Collection("users")
		favCol := cfg.MongoClient.Database(cfg.DBName).Collection("favorites")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// --- Build filter ---
		filter := bson.M{}
		if q := c.Query("q"); q != "" {
			filter["title"] = bson.M{"$regex": q, "$options": "i"}
		}

		cursor, err := hubCol.Find(ctx, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch hubs"})
			return
		}

		var hubs []models.Hub
		if err := cursor.All(ctx, &hubs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not decode hubs"})
			return
		}

		for i, hub := range hubs {
			// --- Fetch Reviews for this Hub ---
			var reviews []models.Review
			reviewCursor, err := reviewCol.Find(ctx, bson.M{"hub_id": hub.ID})
			if err == nil {
				_ = reviewCursor.All(ctx, &reviews)
			}

			// --- Enrich Reviews with User Names ---
			var reviewResponses []models.ReviewResponse
			for _, r := range reviews {
				var user struct {
					Name string `bson:"name"`
				}
				err := userCol.FindOne(ctx, bson.M{"_id": r.UserID}).Decode(&user)
				username := "Unknown"
				if err == nil {
					username = user.Name
				}

				reviewResponses = append(reviewResponses, models.ReviewResponse{
					ID:        r.ID,
					UserID:    r.UserID,
					UserName:  username,
					HubID:     r.HubID,
					Rating:    r.Rating,
					Comment:   r.Comment,
					CreatedAt: r.CreatedAt,
				})
			}

			// --- Add Reviews to Hub ---
			hubs[i].Reviews = reviewResponses

			// --- Check if Favorited ---
			err = favCol.FindOne(ctx, bson.M{"user_id": userID, "hub_id": hub.ID}).Err()
			hubs[i].IsFavorite = (err == nil)
		}

		c.JSON(http.StatusOK, hubs)
	}
}

// ---------------- GET ----------------
func GetHub(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// --- Get authenticated user ID (if available) ---
		uid := c.GetString("user_id")
		var userID primitive.ObjectID
		var hasUser bool

		if uid != "" {
			var err error
			userID, err = primitive.ObjectIDFromHex(uid)
			if err == nil {
				hasUser = true
			}
		}

		hubID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// --- Fetch the hub (publicly accessible) ---
		var hub models.Hub
		err = cfg.MongoClient.Database(cfg.DBName).
			Collection("hubs").
			FindOne(ctx, bson.M{"_id": hubID}).
			Decode(&hub)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "hub not found"})
			return
		}

		// --- Fetch reviews for this hub ---
		reviewColl := cfg.MongoClient.Database(cfg.DBName).Collection("reviews")
		userColl := cfg.MongoClient.Database(cfg.DBName).Collection("users")

		cursor, err := reviewColl.Find(ctx, bson.M{"hub_id": hubID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch reviews"})
			return
		}
		defer cursor.Close(ctx)

		type ReviewResponse struct {
			ID        primitive.ObjectID `json:"id"`
			UserName  string             `json:"user_name"`
			Comment   string             `json:"comment"`
			Rating    int                `json:"rating"`
			CreatedAt time.Time          `json:"created_at"`
		}

		var reviews []ReviewResponse

		for cursor.Next(ctx) {
			var review models.Review
			if err := cursor.Decode(&review); err != nil {
				continue
			}

			var user models.User
			if err := userColl.FindOne(ctx, bson.M{"_id": review.UserID}).Decode(&user); err != nil {
				user.Name = "Unknown User"
			}

			reviews = append(reviews, ReviewResponse{
				ID:        review.ID,
				UserName:  user.Name,
				Comment:   review.Comment,
				Rating:    review.Rating,
				CreatedAt: review.CreatedAt,
			})
		}

		// --- Check if the current user favorited this hub ---
		isFavorite := false
		if hasUser {
			favColl := cfg.MongoClient.Database(cfg.DBName).Collection("favorites")
			count, err := favColl.CountDocuments(ctx, bson.M{"hub_id": hubID, "user_id": userID})
			if err == nil && count > 0 {
				isFavorite = true
			}
		}

		// --- ETag handling ---
		etag := utils.GenerateETag(hub.ID, hub.UpdatedAt)
		if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
			c.Status(http.StatusNotModified)
			return
		}
		c.Header("ETag", etag)

		// --- Response ---
		c.JSON(http.StatusOK, gin.H{
			"hub":        hub,
			"reviews":    reviews,
			"is_favorite": isFavorite,
		})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update hub"})
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
			"hub":   updated,
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

		// ✅ Delete hub
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
			"message": "Hub deleted successfully",
			"id":      oid.Hex(),
		})
	}
}

func AddReview(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDHex := c.GetString("user_id")
		userID, err := primitive.ObjectIDFromHex(userIDHex)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		hubID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		var input struct {
			Rating  int    `json:"rating" binding:"required,min=1,max=5"`
			Comment string `json:"comment"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		review := models.Review{
			ID:        primitive.NewObjectID(),
			UserID:    userID,
			HubID:     hubID,
			Rating:    input.Rating,
			Comment:   input.Comment,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		col := cfg.MongoClient.Database(cfg.DBName).Collection("reviews")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := col.InsertOne(ctx, review); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not add review"})
			return
		}

		c.JSON(http.StatusCreated, review)
	}
}


func ToggleFavorite(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDHex := c.GetString("user_id")
		userID, err := primitive.ObjectIDFromHex(userIDHex)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		hubID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		col := cfg.MongoClient.Database(cfg.DBName).Collection("favorites")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// ✅ Check if already favorited
		var fav models.Favorite
		err = col.FindOne(ctx, bson.M{"user_id": userID, "hub_id": hubID}).Decode(&fav)

		if err == nil {
			// ✅ Unfavorite
			_, _ = col.DeleteOne(ctx, bson.M{"_id": fav.ID})
			c.JSON(http.StatusOK, gin.H{
				"message":  "removed from favorites",
				"favorite": false, // ← now returns false
			})
			return
		}

		// ✅ Add to favorites
		favorite := models.Favorite{
			ID:        primitive.NewObjectID(),
			UserID:    userID,
			HubID:     hubID,
			CreatedAt: time.Now(),
		}

		if _, err := col.InsertOne(ctx, favorite); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not mark as favorite"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":  "added to favorites",
			"favorite": true, // ← now returns true
		})
	}
}



func ListFavorites(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDHex := c.GetString("user_id")
		userID, err := primitive.ObjectIDFromHex(userIDHex)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id"})
			return
		}

		favCol := cfg.MongoClient.Database(cfg.DBName).Collection("favorites")
		hubCol := cfg.MongoClient.Database(cfg.DBName).Collection("hubs")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cursor, err := favCol.Find(ctx, bson.M{"user_id": userID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch favorites"})
			return
		}

		var favs []models.Favorite
		if err := cursor.All(ctx, &favs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not decode favorites"})
			return
		}

		hubIDs := []primitive.ObjectID{}
		for _, f := range favs {
			hubIDs = append(hubIDs, f.HubID)
		}

		cursor, err = hubCol.Find(ctx, bson.M{"_id": bson.M{"$in": hubIDs}})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch hubs"})
			return
		}

		var hubs []models.Hub
		if err := cursor.All(ctx, &hubs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not decode hubs"})
			return
		}

		c.JSON(http.StatusOK, hubs)
	}
}
