package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Structs ---

type PartManifest struct {
	PartName string `bson:"part_name"`
	Quantity int    `bson:"quantity"`
}

type Batch struct {
	BatchAllocationID string `bson:"batch_allocation_id"`
	VendorDetails     struct {
		VendorID string `bson:"vendor_id"`
	} `bson:"vendor_details"`
	PartsManifest []PartManifest `bson:"parts_manifest"`
}

type LogEntry struct {
	VehicleID string `bson:"vehicleId"`
	Data      struct {
		Component string `bson:"component"`
	} `bson:"data"`
}

type LocalMetrics struct {
	TotalJobs          int      `json:"total_jobs" bson:"total_jobs"`
	FailedJobs         int      `json:"failed_jobs" bson:"failed_jobs"`
	DurabilityScore    int      `json:"durability_score" bson:"durability_score"`
	CompanyLocalRating float64  `json:"company_local_rating" bson:"company_local_rating"`
	AvgResponseTime    int      `json:"avg_response_time" bson:"avg_response_time"`
	CompanyReviews     []string `json:"company_reviews" bson:"company_reviews"`
	AvgRating          float64  `json:"avg_rating" bson:"avg_rating"`
}

type Vendor struct {
	VendorID     string       `json:"vendor_id" bson:"vendor_id"`
	Name         string       `json:"name" bson:"name"`
	Category     string       `json:"category" bson:"category"`
	LocalMetrics LocalMetrics `json:"local_metrics" bson:"local_metrics"`
}

var client *mongo.Client
var vendorCol *mongo.Collection
var batchCol *mongo.Collection
var logsCol *mongo.Collection

func connectDB() {
	godotenv.Load()
	uri := os.Getenv("MONGO_URI")
	c, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	client = c
	vendorCol = client.Database("auto_ai_db").Collection("vendors")
	batchCol = client.Database("auto_ai_db").Collection("batches")
	logsCol = client.Database("techathon_db").Collection("Logs")
	fmt.Println("Connected to Databases")
}

func runBackgroundMetrics(vendorID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("🚀 Starting background metrics calculation for: %s\n", vendorID)

	// 1. Fetch Vendor current data (needed for local rating)
	var currentVendor Vendor
	err := vendorCol.FindOne(ctx, bson.M{"vendor_id": vendorID}).Decode(&currentVendor)
	if err != nil {
		fmt.Printf("❌ Error finding vendor: %v\n", err)
		return
	}

	// 2. Fetch Batches
	batchCursor, _ := batchCol.Find(ctx, bson.M{"vendor_details.vendor_id": vendorID})
	var batches []Batch
	batchCursor.All(ctx, &batches)

	// 3. Fetch Logs
	logCursor, _ := logsCol.Find(ctx, bson.M{})
	var allLogs []LogEntry
	logCursor.All(ctx, &allLogs)

	totalJobs := len(batches)
	failedJobs := 0
	batchTotalQtyMap := make(map[string]int)
	batchFailuresMap := make(map[string]int)

	for _, b := range batches {
		qty := 0
		for _, p := range b.PartsManifest { qty += p.Quantity }
		batchTotalQtyMap[b.BatchAllocationID] = qty
	}

	for _, l := range allLogs {
		parts := strings.Split(l.VehicleID, "#")
		if len(parts) < 1 { continue }
		logBatchID := parts[0]

		for _, b := range batches {
			if b.BatchAllocationID == logBatchID {
				for _, p := range b.PartsManifest {
					if strings.EqualFold(p.PartName, l.Data.Component) {
						batchFailuresMap[logBatchID]++
					}
				}
			}
		}
	}

	for id, failCount := range batchFailuresMap {
		total := batchTotalQtyMap[id]
		if total > 0 && float64(failCount) > (float64(total)*0.1) {
			failedJobs++
		}
	}

	// --- 4. NEW CALCULATIONS ---
	durabilityScore := 100 // Default if no jobs
	if totalJobs > 0 {
		durabilityScore = ((totalJobs - failedJobs) * 100) / totalJobs
	}

	// avg_rating = (durability_score + 10 * local_rating) / 2
	localRating := currentVendor.LocalMetrics.CompanyLocalRating
	avgRating := (float64(durabilityScore) + (10.0 * localRating)) / 2.0

	// 5. Final Patch Update
	update := bson.M{
		"$set": bson.M{
			"local_metrics.total_jobs":       totalJobs,
			"local_metrics.failed_jobs":      failedJobs,
			"local_metrics.durability_score": durabilityScore,
			"local_metrics.avg_rating":       avgRating,
		},
	}
	
	_, err = vendorCol.UpdateOne(ctx, bson.M{"vendor_id": vendorID}, update)
	if err != nil {
		fmt.Printf("❌ Background update failed for %s: %v\n", vendorID, err)
	} else {
		fmt.Printf("✅ Background metrics updated for %s: Durability(%d), Rating(%.2f)\n", vendorID, durabilityScore, avgRating)
	}
}

func handleOnboarding(c *gin.Context) {
	var vendor Vendor
	if err := c.ShouldBindJSON(&vendor); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := vendorCol.UpdateOne(
		context.TODO(),
		bson.M{"vendor_id": vendor.VendorID},
		bson.M{"$set": vendor},
		options.Update().SetUpsert(true),
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save initial data"})
		return
	}

	go runBackgroundMetrics(vendor.VendorID)

	c.JSON(http.StatusAccepted, gin.H{
		"message":   "Vendor data received. Durability and Rating are being calculated.",
		"vendor_id": vendor.VendorID,
	})
}

func main() {
	connectDB()
	r := gin.Default()
	r.POST("/api/vendor", handleOnboarding)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "alive"})
	})
	r.Run(":8080")
}