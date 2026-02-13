# Kopi Order 🏪☕

A web application for managing group drink orders in Singapore kopitiam style!

## Features

### 🎯 Three Drink Categories

1. **Brewed Drinks (Kopi/Teh)** - Complex modifier system
   - Base: Kopi, Teh, Yuan Yang, Milo
   - Milk: Standard, O (no milk), C (evaporated)
   - Sweetness: Standard, Siu Dai (less sugar), Ga Dai (more sugar), Kosong (no sugar)
   - Strength: Standard, Gao (thick), Po (thin), Di Lo (extra thick)
   - Temperature: Hot, Peng (iced), Pua Sio (lukewarm)

2. **Others** - Simple temperature selection
   - Lemon Tea, Barley, Lime Juice
   - Hot/Cold or Warm/Cold options

3. **Canned/Packet Drinks**
   - 100 Plus, Coke, Coke Zero, Sprite, Kickapoo
   - Green Tea, Oolong Tea, Chrysanthemum Tea

4. **Write-in** - Custom orders

### 📱 User Flow

1. **Landing Page** - Start a new kopitiam run
2. **Order Page** - Add nickname and build your drinks
3. **Admin View** - Two views for order management:
   - **Consolidated View** (Stall-friendly): Large text, aggregated by drink
   - **Distribution View** (Office-friendly): By person, with delivery tracking

### 🔗 Sharing

- QR Code generation
- Shareable link
- No authentication needed (security via secret URL)

## Tech Stack

- **Backend**: Go 1.21+ with Chi router
- **Frontend**: HTML templates + Tailwind CSS + HTMX
- **Database**: SQLite with GORM
- **Utilities**: QR code generation

## Setup & Run

### Prerequisites

- Go 1.21 or higher
- Internet connection (for CDN resources)

### Installation

1. Clone or download this project

2. Install dependencies:
```bash
go mod download
```

3. Run the application:
```bash
go run main.go
```

4. Open your browser to:
```
http://localhost:8080
```

## Usage

### Starting a Kopitiam Run

1. Go to the landing page
2. Click "Start Kopitiam Run"
3. You'll be redirected to the Admin view with a unique room

### Sharing with Your Group

1. From the Admin view, either:
   - Show the QR code for people to scan
   - Click "Copy Link" and share via WhatsApp/Telegram/Slack

### Placing Orders

1. Users click the shared link
2. Enter their nickname (saved in browser)
3. Select drinks using the intuitive builder:
   - **Kopi/Teh tab**: Build complex drinks with modifiers
   - **Others tab**: Quick selection with temperature
   - **Canned tab**: Simple grid selection
   - **Write-in tab**: Type anything custom
4. Add multiple items to cart
5. Submit all orders at once

### Managing Orders (Admin)

1. **Consolidated View** - Perfect for showing kopitiam uncle:
   - "5x Kopi O Di Lo"
   - "2x Barley (Warm)"
   - Large, bold text for easy reading

2. **Distribution View** - Track delivery to colleagues:
   - See who ordered what
   - Checkbox to mark as delivered
   - Auto-refreshes every 5 seconds

## Examples

### Brewed Drinks
- "Kopi" - Standard coffee with condensed milk
- "Kopi O" - Coffee without milk
- "Kopi C Siu Dai" - Coffee with evaporated milk, less sugar
- "Teh O Di Lo Peng" - Tea without milk, extra thick, iced
- "Yuan Yang Gao" - Coffee-tea mix, thick

### Others
- "Lemon Tea (Hot)"
- "Barley (Warm)"
- "Lime Juice (Cold)"

### Canned
- "100 Plus"
- "Coke Zero"
- "Green Tea"

## File Structure

```
kopitiam-run/
├── main.go                           # Backend logic
├── go.mod                            # Dependencies
├── templates/
│   ├── landing.html                  # Landing page
│   ├── order.html                    # Order placement page
│   ├── admin.html                    # Admin control panel
│   ├── consolidated_view.html        # Stall-friendly view
│   └── distribution_view.html        # Office-friendly view
└── kopitiam.db                       # SQLite database (auto-created)
```

## API Endpoints

- `GET /` - Landing page
- `POST /room/create` - Create new room
- `GET /order/:roomID` - Order page for users
- `POST /order/:roomID` - Submit order
- `GET /room/:roomID/admin` - Admin view
- `GET /room/:roomID/orders?view=consolidated|distribution` - Get orders
- `POST /room/:roomID/orders/:orderID/toggle` - Toggle delivery status
- `GET /qr/:roomID` - Generate QR code

## Database Schema

### Rooms
- `id` (string, primary key) - 8-character UUID
- `created_at` (timestamp)

### Orders
- `id` (uint, primary key)
- `room_id` (string, foreign key)
- `nickname` (string)
- `drink_type` (string) - brewed/others/canned/writein
- `drink_name` (string) - Full drink description
- `delivered` (boolean)
- `created_at` (timestamp)

## Mobile-First Design

- Large touch targets (minimum 44x44px)
- Responsive layout
- Works on all screen sizes
- Optimized for one-handed use

## Future Enhancements

Possible additions:
- Price tracking
- Order history
- Export to Excel/PDF
- WhatsApp integration
- Multi-language support (Chinese, Malay, Tamil)
- Payment splitting

## License

MIT

## Contributing

Pull requests welcome! Please maintain the Singaporean kopitiam spirit 🇸🇬

---

Made with ❤️ for all the coffee-loving teams in Singapore