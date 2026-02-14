package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/glebarez/sqlite" // Pure go driver
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
	"gorm.io/gorm"
)

//go:embed templates/* static/*
var embeddedFS embed.FS

var (
	db        *gorm.DB
	templates *template.Template
)

// Models
type Room struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Locked    bool      `gorm:"default:false" json:"locked"`
	CreatedAt time.Time `json:"created_at"`
}

type Order struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	RoomID    string    `gorm:"index" json:"room_id"`
	Nickname  string    `json:"nickname"`
	DrinkType string    `json:"drink_type"` // brewed, others, canned, writein
	DrinkName string    `json:"drink_name"`
	Delivered bool      `gorm:"default:false" json:"delivered"`
	CreatedAt time.Time `json:"created_at"`
}

// DTOs
type OrderRequest struct {
	Nickname  string `json:"nickname"`
	DrinkType string `json:"drink_type"`
	DrinkName string `json:"drink_name"`
}

type BatchOrderRequest struct {
	Nickname string `json:"nickname"`
	Orders   []struct {
		// Use the same names as the frontend cart item for easy mapping
		DrinkType string `json:"type"`
		DrinkName string `json:"name"`
	} `json:"orders"`
}

type ConsolidatedDrink struct {
	Name  string
	Count int
	IDs   []uint
}

type UserDrink struct {
	OrderID   uint
	Nickname  string
	DrinkName string
	Delivered bool
}

func main() {
	// Initialize database
	var err error

	// Make DB path flexible for local dev vs. container.
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		// In a container, a volume is mounted at /data.
		// For local dev, we'll just create the db in the current directory.
		if _, err := os.Stat("/data"); !os.IsNotExist(err) {
			dbPath = "/data/kopi-order.db"
		} else {
			dbPath = "kopi-order.db"
			log.Println("ℹ️ /data directory not found, using local 'kopi-order.db'.")
		}
	}
	db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database at %s: %v", dbPath, err)
	}
	log.Println("✓ Database connected")

	// Auto migrate
	if err := db.AutoMigrate(&Room{}, &Order{}); err != nil {
		log.Fatal("Failed to migrate database:", err)
	}
	log.Println("✓ Database migrated")

	// Parse templates
	templatesFS, _ := fs.Sub(embeddedFS, "templates")
	templates = template.New("").Funcs(template.FuncMap{
		"getDrinkIcon": func(drinkName string) string {
			lowerName := strings.ToLower(drinkName)
			// Check for explicit cold modifiers
			if strings.Contains(lowerName, "peng") || strings.Contains(lowerName, "(cold)") {
				return "🥤"
			}
			// Check for canned drinks which are implicitly cold
			cannedDrinks := []string{
				"coke", "sprite", "100 plus", "kickapoo", "green tea", "oolong tea", "coke zero", "chrysanthemum",
			}
			for _, canned := range cannedDrinks {
				if strings.Contains(lowerName, canned) {
					return "🥤"
				}
			}
			return "☕" // Default to hot drink icon
		},
	})
	templates = template.Must(templates.ParseFS(templatesFS, "*.html", "*.template"))
	log.Println("✓ Templates loaded")

	// Start the background cleanup job
	go startCleanupJob()

	// Setup router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(ProxyHeaders)

	// Serve static files from the embedded filesystem
	staticFS, _ := fs.Sub(embeddedFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Routes
	r.Get("/", handleLanding)
	r.Get("/debug", handleDebug)
	r.Post("/room/create", handleCreateRoom)
	r.Get("/order/{roomID}", handleOrderPage)
	r.Post("/order/{roomID}", handleSubmitOrder)
	r.Post("/order/{roomID}/batch", handleBatchSubmitOrder)
	r.Get("/room/{roomID}/admin", handleAdminView)
	r.Get("/room/{roomID}/orders", handleGetOrders)
	r.Post("/room/{roomID}/orders/{orderID}/toggle", handleToggleDelivered)
	r.Post("/room/{roomID}/toggle-lock", handleToggleLock)
	r.Delete("/order/{roomID}/{orderID}", handleDeleteOrder)
	r.Get("/qr/{roomID}", handleQRCode)
	r.Get("/health", handleHealthCheck)
	r.Get("/manifest.json", handleManifest)

	// --- Graceful Shutdown Setup ---
	server := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	// Channel to listen for OS signals (e.g., Ctrl+C)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Goroutine to start the server, so it doesn't block the main thread.
	go func() {
		log.Println("🚀 Server starting on http://localhost:8080")
		log.Println("📱 Open http://localhost:8080 in your browser")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Block main thread until a shutdown signal is received.
	<-stop

	log.Println("🚦 Shutting down server...")

	// Create a context with a timeout to allow existing connections to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("✅ Server gracefully stopped")
}

// ProxyHeaders is a middleware that adjusts the request scheme and host
// based on `X-Forwarded-Proto` and `X-Forwarded-Host` headers.
// This is crucial for generating correct absolute URLs when behind a reverse proxy
// like Dokku, Heroku, or Cloudflare.
func ProxyHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set scheme from X-Forwarded-Proto header
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			r.URL.Scheme = proto
		} else if r.TLS != nil {
			r.URL.Scheme = "https"
		} else {
			r.URL.Scheme = "http"
		}

		// Set host from X-Forwarded-Host header
		if host := r.Header.Get("X-Forwarded-Host"); host != "" {
			r.Host = host
		}

		next.ServeHTTP(w, r)
	})
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	// A simple health check that just returns 200 OK.
	// This gives Fly.io a lightweight endpoint to confirm the app is running.
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func startCleanupJob() {
	log.Println("🧹 Starting background cleanup job...")
	// Run the cleanup job immediately on start, and then every 7 days.
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()

	// Perform an initial cleanup on startup
	cleanupOldRooms()

	for range ticker.C {
		cleanupOldRooms()
	}
}

func cleanupOldRooms() {
	log.Println("🧹 Running weekly cleanup of old rooms...")
	const retentionPeriod = 7 * 24 * time.Hour // Keep data for 7 days
	cutoff := time.Now().Add(-retentionPeriod)

	var oldRoomIDs []string
	// Find rooms older than the retention period
	if err := db.Model(&Room{}).Where("created_at < ?", cutoff).Pluck("id", &oldRoomIDs).Error; err != nil {
		log.Printf("❌ Error finding old rooms for cleanup: %v", err)
		return
	}

	if len(oldRoomIDs) == 0 {
		log.Println("🧹 No old rooms to clean up.")
		return
	}

	log.Printf("🧹 Found %d old rooms to delete. Deleting associated orders and rooms...", len(oldRoomIDs))
	db.Where("room_id IN ?", oldRoomIDs).Delete(&Order{})
	db.Where("id IN ?", oldRoomIDs).Delete(&Room{})

	log.Println("🧹 Running VACUUM to reclaim disk space...")
	db.Exec("VACUUM")
}

func handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	templates.ExecuteTemplate(w, "manifest.json.template", nil)
}

func handleLanding(w http.ResponseWriter, r *http.Request) {
	templates.ExecuteTemplate(w, "landing.html", nil)
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	templates.ExecuteTemplate(w, "debug.html", nil)
}

func handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	log.Println("📝 Room creation request received")
	log.Printf("   Method: %s", r.Method)
	log.Printf("   HX-Request header: %s", r.Header.Get("HX-Request"))
	log.Printf("   User-Agent: %s", r.Header.Get("User-Agent"))

	roomID := uuid.New().String()[:8]
	log.Printf("   Generated room ID: %s", roomID)

	room := Room{
		ID:        roomID,
		CreatedAt: time.Now(),
	}

	if err := db.Create(&room).Error; err != nil {
		log.Printf("❌ Error creating room: %v", err)
		http.Error(w, "Failed to create room", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Room created successfully: %s", roomID)

	// For HTMX request
	if r.Header.Get("HX-Request") == "true" {
		log.Printf("   Using HTMX redirect to: /room/%s/admin", roomID)
		w.Header().Set("HX-Redirect", "/room/"+roomID+"/admin")
		w.WriteHeader(http.StatusOK)
		return
	}

	// For regular request (fallback)
	log.Printf("   Using HTTP redirect to: /room/%s/admin", roomID)
	http.Redirect(w, r, "/room/"+roomID+"/admin", http.StatusSeeOther)
}

func handleOrderPage(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")

	// Verify room exists
	var room Room
	if err := db.First(&room, "id = ?", roomID).Error; err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	data := map[string]interface{}{
		"RoomID": roomID,
		"Locked": room.Locked,
	}
	templates.ExecuteTemplate(w, "order.html", data)
}

func handleSubmitOrder(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")

	var req OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	order := Order{
		RoomID:    roomID,
		Nickname:  req.Nickname,
		DrinkType: req.DrinkType,
		DrinkName: req.DrinkName,
	}
	db.Create(&order)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleBatchSubmitOrder(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")

	// Check if room is locked
	var room Room
	if err := db.First(&room, "id = ?", roomID).Error; err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	// If the room is locked, no new orders can be placed.
	if room.Locked {
		http.Error(w, "Orders are locked by the admin and new orders cannot be placed.", http.StatusForbidden)
		return
	}

	var req BatchOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid batch request", http.StatusBadRequest)
		return
	}

	// Use a transaction to ensure all orders are created or none are.
	// This makes the batch submission atomic.
	tx := db.Begin()
	if tx.Error != nil {
		log.Printf("❌ Error starting transaction: %v", tx.Error)
		http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
		return
	}

	for _, item := range req.Orders {
		order := Order{
			RoomID:    roomID,
			Nickname:  req.Nickname,
			DrinkType: item.DrinkType,
			DrinkName: item.DrinkName,
		}
		if err := tx.Create(&order).Error; err != nil {
			tx.Rollback() // Rollback on any error
			log.Printf("❌ Error creating order in batch, rolling back: %v", err)
			http.Error(w, "Failed to create order", http.StatusInternalServerError)
			return
		}
	}

	tx.Commit()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleAdminView(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")

	// Verify room exists
	var room Room
	if err := db.First(&room, "id = ?", roomID).Error; err != nil {
		log.Printf("Room not found: %s, error: %v", roomID, err)
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	// Generate order link. The scheme and host are corrected by the ProxyHeaders middleware.
	orderLink := fmt.Sprintf("%s://%s/order/%s", r.URL.Scheme, r.Host, roomID)

	data := map[string]interface{}{
		"RoomID":    roomID,
		"OrderLink": orderLink,
		"Locked":    room.Locked,
	}

	if err := templates.ExecuteTemplate(w, "admin.html", data); err != nil {
		log.Printf("Error rendering admin template: %v", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

func handleGetOrders(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	view := r.URL.Query().Get("view") // "consolidated" or "distribution"

	var orders []Order
	db.Where("room_id = ?", roomID).Order("created_at asc").Find(&orders)

	// Check if the client accepts JSON, and if so, return a JSON response.
	// This allows the order page to fetch data without getting a full HTML template.
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		var data interface{}
		if view == "consolidated" {
			data = consolidateOrders(orders)
		} else {
			// For 'distribution' view, which is what the order page will request
			distribution := distributeOrders(orders)
			data = map[string]interface{}{
				"Orders": distribution,
				"RoomID": roomID,
			}
		}
		json.NewEncoder(w).Encode(data)
		return
	}

	if view == "consolidated" {
		consolidated := consolidateOrders(orders)
		templates.ExecuteTemplate(w, "consolidated_view.html", consolidated)
	} else {
		distribution := distributeOrders(orders)
		data := map[string]interface{}{
			"Orders": distribution,
			"RoomID": roomID,
		}
		templates.ExecuteTemplate(w, "distribution_view.html", data)
	}
}

func consolidateOrders(orders []Order) []ConsolidatedDrink {
	drinkMap := make(map[string]*ConsolidatedDrink)

	for _, order := range orders {
		if existing, exists := drinkMap[order.DrinkName]; exists {
			existing.Count++
			existing.IDs = append(existing.IDs, order.ID)
		} else {
			drinkMap[order.DrinkName] = &ConsolidatedDrink{
				Name:  order.DrinkName,
				Count: 1,
				IDs:   []uint{order.ID},
			}
		}
	}

	// Convert map to slice
	result := make([]ConsolidatedDrink, 0, len(drinkMap))
	for _, drink := range drinkMap {
		result = append(result, *drink)
	}

	// Sort the slice by drink name to ensure a stable order on every refresh.
	// This prevents the list from re-ordering on auto-refresh.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func distributeOrders(orders []Order) []UserDrink {
	result := make([]UserDrink, 0, len(orders))

	for _, order := range orders {
		result = append(result, UserDrink{
			OrderID:   order.ID,
			Nickname:  order.Nickname,
			DrinkName: order.DrinkName,
			Delivered: order.Delivered,
		})
	}

	return result
}

func handleToggleDelivered(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	orderID := chi.URLParam(r, "orderID")

	var order Order
	if err := db.First(&order, "id = ? AND room_id = ?", orderID, roomID).Error; err != nil {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	order.Delivered = !order.Delivered
	db.Save(&order)

	// After toggling, we need to return the entire updated list for the HTMX swap.
	// This prevents race conditions with the auto-refresh and provides instant feedback.
	var orders []Order
	db.Where("room_id = ?", roomID).Order("created_at asc").Find(&orders)

	distribution := distributeOrders(orders)
	data := map[string]interface{}{
		"Orders": distribution,
		"RoomID": roomID,
	}
	w.Header().Set("HX-Trigger", "ordersChanged")
	templates.ExecuteTemplate(w, "distribution_view.html", data)
}

func handleToggleLock(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")

	var room Room
	if err := db.First(&room, "id = ?", roomID).Error; err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	room.Locked = !room.Locked
	db.Save(&room)

	// Return the updated control fragment
	data := map[string]interface{}{
		"RoomID": roomID,
		"Locked": room.Locked,
	}
	templates.ExecuteTemplate(w, "lock_controls.html", data)
}

func handleDeleteOrder(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")
	orderID := chi.URLParam(r, "orderID")

	var room Room
	if err := db.First(&room, "id = ?", roomID).Error; err != nil {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	isHtmxRequest := r.Header.Get("HX-Request") == "true"
	if room.Locked && !isHtmxRequest {
		http.Error(w, "Orders are locked by the admin and cannot be deleted.", http.StatusForbidden)
		return
	}
	// Use a transaction for safety.
	tx := db.Begin()
	result := tx.Delete(&Order{}, "id = ? AND room_id = ?", orderID, roomID)
	if result.Error != nil {
		tx.Rollback()
		log.Printf("❌ Error deleting order: %v", result.Error)
		http.Error(w, "Failed to delete order", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		// This isn't strictly an error, but the client might have sent a stale ID.
		// Responding with 404 is appropriate.
		http.Error(w, "Order not found or does not belong to this room", http.StatusNotFound)
		return
	}
	tx.Commit()

	// For HTMX requests (from admin page), return the updated distribution list.
	if r.Header.Get("HX-Request") == "true" {
		var orders []Order
		db.Where("room_id = ?", roomID).Order("created_at asc").Find(&orders)
		distribution := distributeOrders(orders)
		data := map[string]interface{}{
			"Orders": distribution,
			"RoomID": roomID,
		}
		w.Header().Set("HX-Trigger", "ordersChanged")
		templates.ExecuteTemplate(w, "distribution_view.html", data)
		return
	}

	// For regular API requests (from order page), return success with no content.
	w.WriteHeader(http.StatusNoContent)
}

func handleQRCode(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "roomID")

	// The scheme and host are corrected by the ProxyHeaders middleware.
	orderLink := fmt.Sprintf("%s://%s/order/%s", r.URL.Scheme, r.Host, roomID)

	qr, err := qrcode.Encode(orderLink, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	// Return base64 encoded image for embedding
	encoded := base64.StdEncoding.EncodeToString(qr)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("data:image/png;base64," + encoded))
}
