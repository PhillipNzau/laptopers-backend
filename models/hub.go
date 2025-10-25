package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Coordinates struct for latitude and longitude
type Coordinates struct {
	Lat float64 `bson:"lat" json:"lat"`
	Lng float64 `bson:"lng" json:"lng"`
}

type Hub struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID       primitive.ObjectID `bson:"user_id" json:"user_id"`
	Title        string             `bson:"title" json:"title"`
	Description  string             `bson:"description,omitempty" json:"description,omitempty"`
	Coordinates  Coordinates        `bson:"coordinates,omitempty" json:"coordinates,omitempty"`
	LocationName string             `bson:"location,omitempty" json:"location_name,omitempty"`
	Rating       float64            `bson:"target_amount,omitempty" json:"rating,omitempty"`
	Images       []string           `bson:"images" json:"images"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`

	// Enriched fields
	IsFavorite bool                     `json:"is_favorite,omitempty" bson:"-"`
	Reviews    []ReviewResponse         `json:"reviews,omitempty" bson:"-"`
}


// --- Review ---
type Review struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`
	HubID     primitive.ObjectID `bson:"hub_id" json:"hub_id"`
	Rating    int                `bson:"rating" json:"rating"` // 1–5
	Comment   string             `bson:"comment,omitempty" json:"comment,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// --- Favorite ---
type Favorite struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`
	HubID     primitive.ObjectID `bson:"hub_id" json:"hub_id"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}


type ReviewResponse struct {
	ID        primitive.ObjectID `json:"id"`
	UserID    primitive.ObjectID `json:"user_id"`
	UserName  string             `json:"user_name"`
	HubID     primitive.ObjectID `json:"hub_id"`
	Rating    int                `json:"rating"`
	Comment   string             `json:"comment"`
	CreatedAt time.Time          `json:"created_at"`
}