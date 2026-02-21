package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Struct Definitions ---

type Contact struct {
	Email string `json:"email" bson:"email"`
}

type LocalMetrics struct {
	DurabilityScore    int      `json:"durability_score" bson:"durability_score"`
	CompanyLocalRating float64  `json:"company_local_rating" bson:"company_local_rating"`
	TotalJobs          int      `json:"total_jobs" bson:"total_jobs"`
	FailedJobs         int      `json:"failed_jobs" bson:"failed_jobs"`
	AvgResponseTime    int      `json:"avg_response_time" bson:"avg_response_time"`
	CompanyReviews     []string `json:"company_reviews" bson:"company_reviews"`
	AvgRating          float64  `json:"avg_rating" bson:"avg_rating"`
}

type Vendor struct {
	VendorID     string       `json:"vendor_id" bson:"vendor_id"`
	Name         string       `json:"name" bson:"name"`
	Category     string       `json:"category" bson:"category"`
	Contact      Contact      `json:"contact" bson:"contact"`
	LocalMetrics LocalMetrics `json:"local_metrics" bson:"local_metrics"`
}

var collection *mongo.Collection

// --- Database Setup ---
func connectDB() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	uri := os.Getenv("MONGO_URI")
	dbName := os.Getenv("DB_NAME")
	colName := os.Getenv("COLLECTION_NAME")

	clientOptions := options.Client().ApplyURI(uri)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal("Could not connect to MongoDB:", err)
	}

	fmt.Printf("Connected to MongoDB! Database: %s, Collection: %s\n", dbName, colName)
	collection = client.Database(dbName).Collection(colName)
}

// --- Handlers ---
func createVendor(c *gin.Context) {
	var vendor Vendor

	if err := c.ShouldBindJSON(&vendor); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := collection.InsertOne(ctx, vendor)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store vendor data"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":    "Vendor stored successfully in auto_ai_db",
		"insertedId": result.InsertedID,
		"vendor_id":  vendor.VendorID,
	})
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "active", "service": "vendor-onboarding"})
}

func main() {
	connectDB()

	r := gin.Default()

	// Routes
	r.GET("/health", healthCheck)
	r.POST("/api/vendor", createVendor)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	r.Run(":" + port)
}