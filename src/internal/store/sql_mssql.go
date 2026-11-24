package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "github.com/microsoft/go-mssqldb"

	"cakefactory/internal/model"
)

// MSSQL is the Microsoft SQL Server store.
type MSSQL struct {
	dsn string
	db  *sql.DB
}

// NewMSSQL creates a store for a DSN like
// sqlserver://sa:password@host:1433?database=cakefactory.
func NewMSSQL(dsn string) *MSSQL { return &MSSQL{dsn: dsn} }

const schema = `
IF OBJECT_ID(N'dbo.recipes','U') IS NULL CREATE TABLE dbo.recipes (
  name NVARCHAR(100) PRIMARY KEY, mix_seconds INT NOT NULL, dose_tol_pct FLOAT NOT NULL,
  pans_per_batch INT NOT NULL, fill_target FLOAT NOT NULL, fill_tol FLOAT NOT NULL);
IF OBJECT_ID(N'dbo.recipe_items','U') IS NULL CREATE TABLE dbo.recipe_items (
  recipe NVARCHAR(100) NOT NULL REFERENCES dbo.recipes(name), ingredient NVARCHAR(100) NOT NULL,
  grams FLOAT NOT NULL, PRIMARY KEY (recipe, ingredient));
IF OBJECT_ID(N'dbo.ingredient_stock','U') IS NULL CREATE TABLE dbo.ingredient_stock (
  ingredient NVARCHAR(100) PRIMARY KEY, grams FLOAT NOT NULL, reserved FLOAT NOT NULL DEFAULT 0, organic BIT NOT NULL);
IF OBJECT_ID(N'dbo.reservations','U') IS NULL CREATE TABLE dbo.reservations (
  order_id NVARCHAR(50) NOT NULL, ingredient NVARCHAR(100) NOT NULL, grams FLOAT NOT NULL,
  consumed BIT NOT NULL DEFAULT 0, PRIMARY KEY (order_id, ingredient));
IF OBJECT_ID(N'dbo.production_orders','U') IS NULL CREATE TABLE dbo.production_orders (
  id NVARCHAR(50) PRIMARY KEY, recipe NVARCHAR(100) NOT NULL, status NVARCHAR(30) NOT NULL,
  created DATETIME2 NOT NULL, note NVARCHAR(400) NULL);
IF OBJECT_ID(N'dbo.shipments','U') IS NULL CREATE TABLE dbo.shipments (
  pallet_id NVARCHAR(50) PRIMARY KEY, cakes INT NOT NULL, batches NVARCHAR(400) NOT NULL,
  vehicle NVARCHAR(50) NOT NULL, at DATETIME2 NOT NULL);
IF OBJECT_ID(N'dbo.event_log','U') IS NULL CREATE TABLE dbo.event_log (
  id BIGINT IDENTITY PRIMARY KEY, at DATETIME2 NOT NULL, node NVARCHAR(10) NOT NULL, device NVARCHAR(100) NULL,
  severity NVARCHAR(10) NOT NULL, code NVARCHAR(20) NULL, reason NVARCHAR(400) NOT NULL,
  batch_id NVARCHAR(50) NULL, machine_id NVARCHAR(100) NULL, maintenance BIT NOT NULL);
IF OBJECT_ID(N'dbo.maintenance_tickets','U') IS NULL CREATE TABLE dbo.maintenance_tickets (
  id BIGINT IDENTITY PRIMARY KEY, opened DATETIME2 NOT NULL, closed DATETIME2 NULL, node NVARCHAR(10) NOT NULL,
  machine_id NVARCHAR(100) NOT NULL, reason NVARCHAR(400) NOT NULL, status NVARCHAR(20) NOT NULL);
`

// Init creates the database, the tables and the seed data.
func (s *MSSQL) Init(ctx context.Context) error {
	u, err := url.Parse(s.dsn)
	if err != nil {
		return err
	}
	dbName := u.Query().Get("database")
	if dbName == "" {
		dbName = "cakefactory"
	}
	q := u.Query()
	q.Set("database", "master")
	u.RawQuery = q.Encode()
	master, err := sql.Open("sqlserver", u.String())
	if err != nil {
		return err
	}
	defer master.Close()
	if _, err := master.ExecContext(ctx, fmt.Sprintf("IF DB_ID(N'%s') IS NULL CREATE DATABASE [%s]", dbName, dbName)); err != nil {
		return err
	}
	if s.db != nil {
		s.db.Close()
	}
	db, err := sql.Open("sqlserver", s.dsn)
	if err != nil {
		return err
	}
	s.db = db
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "DECLARE @r INT; EXEC @r = sp_getapplock @Resource='cakefactory-init', @LockMode='Exclusive', @LockOwner='Session', @LockTimeout=30000; IF @r < 0 RAISERROR('init lock timeout',16,1)"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "EXEC sp_releaseapplock @Resource='cakefactory-init', @LockOwner='Session'")
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	return s.seed(ctx)
}

func (s *MSSQL) seed(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM dbo.recipes").Scan(&n); err != nil {
		return err
	}
	r := SeedRecipe
	if n > 0 { // an older database: bring the recipe up to the seed values (stock is left alone)
		if _, err := s.db.ExecContext(ctx, "UPDATE dbo.recipes SET mix_seconds=@p2, dose_tol_pct=@p3, pans_per_batch=@p4, fill_target=@p5, fill_tol=@p6 WHERE name=@p1",
			r.Name, r.MixSeconds, r.DoseTolPct, r.PansPerBatch, r.FillTarget, r.FillTol); err != nil {
			return err
		}
		for _, it := range r.Items {
			if _, err := s.db.ExecContext(ctx, "UPDATE dbo.recipe_items SET grams=@p3 WHERE recipe=@p1 AND ingredient=@p2", r.Name, it.Ingredient, it.Grams); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "INSERT dbo.recipes VALUES (@p1,@p2,@p3,@p4,@p5,@p6)",
		r.Name, r.MixSeconds, r.DoseTolPct, r.PansPerBatch, r.FillTarget, r.FillTol); err != nil {
		return err
	}
	for _, it := range r.Items {
		if _, err := s.db.ExecContext(ctx, "INSERT dbo.recipe_items VALUES (@p1,@p2,@p3)", r.Name, it.Ingredient, it.Grams); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, "INSERT dbo.ingredient_stock VALUES (@p1,@p2,0,1)", it.Ingredient, float64(SeedStockGrams)); err != nil {
			return err
		}
	}
	return nil
}

// Recipe loads a recipe with its targets.
func (s *MSSQL) Recipe(ctx context.Context, name string) (model.Recipe, error) {
	r := model.Recipe{Name: name}
	err := s.db.QueryRowContext(ctx,
		"SELECT mix_seconds, dose_tol_pct, pans_per_batch, fill_target, fill_tol FROM dbo.recipes WHERE name=@p1", name).
		Scan(&r.MixSeconds, &r.DoseTolPct, &r.PansPerBatch, &r.FillTarget, &r.FillTol)
	if err != nil {
		return r, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT ingredient, grams FROM dbo.recipe_items WHERE recipe=@p1 ORDER BY ingredient", name)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var it model.Item
		if err := rows.Scan(&it.Ingredient, &it.Grams); err != nil {
			return r, err
		}
		r.Items = append(r.Items, it)
	}
	return r, rows.Err()
}

// Reserve reserves ingredients in one transaction, enforcing the organic rule.
func (s *MSSQL) Reserve(ctx context.Context, orderID string, items []model.Item) (bool, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback()
	var have int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM dbo.reservations WHERE order_id=@p1", orderID).Scan(&have); err != nil {
		return false, "", err
	}
	if have > 0 { // repeated request after a restart: same answer
		return true, "", nil
	}
	for _, it := range items {
		var grams, reserved float64
		var organic bool
		err := tx.QueryRowContext(ctx,
			"SELECT grams, reserved, organic FROM dbo.ingredient_stock WITH (UPDLOCK) WHERE ingredient=@p1", it.Ingredient).
			Scan(&grams, &reserved, &organic)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return false, "unknown ingredient " + it.Ingredient, nil
		case err != nil:
			return false, "", err
		case !organic:
			return false, it.Ingredient + " is not organic", nil
		case grams-reserved < it.Grams:
			return false, "insufficient stock of " + it.Ingredient, nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE dbo.ingredient_stock SET reserved=reserved+@p2 WHERE ingredient=@p1", it.Ingredient, it.Grams); err != nil {
			return false, "", err
		}
		if _, err := tx.ExecContext(ctx, "INSERT dbo.reservations (order_id, ingredient, grams) VALUES (@p1,@p2,@p3)", orderID, it.Ingredient, it.Grams); err != nil {
			return false, "", err
		}
	}
	return true, "", tx.Commit()
}

// Consume turns a reservation into a withdrawal and updates the stock.
func (s *MSSQL) Consume(ctx context.Context, orderID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE st SET st.grams=st.grams-r.grams, st.reserved=st.reserved-r.grams
		FROM dbo.ingredient_stock st JOIN dbo.reservations r ON r.ingredient=st.ingredient
		WHERE r.order_id=@p1 AND r.consumed=0`, orderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE dbo.reservations SET consumed=1 WHERE order_id=@p1", orderID); err != nil {
		return err
	}
	return tx.Commit()
}

// Restock adds stock for an ingredient.
func (s *MSSQL) Restock(ctx context.Context, ing string, grams float64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE dbo.ingredient_stock SET grams=grams+@p2 WHERE ingredient=@p1", ing, grams)
	return err
}

// Stock lists the stock of every ingredient.
func (s *MSSQL) Stock(ctx context.Context) ([]model.StockRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT ingredient, grams, reserved, organic FROM dbo.ingredient_stock ORDER BY ingredient")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.StockRow
	for rows.Next() {
		var r model.StockRow
		if err := rows.Scan(&r.Ingredient, &r.Grams, &r.Reserved, &r.Organic); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastOrderNo returns the highest order number stored, so a restarted PLM never
// issues an order id again.
func (s *MSSQL) LastOrderNo(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT ISNULL(MAX(TRY_CAST(SUBSTRING(id, 4, 10) AS INT)), 0) FROM dbo.production_orders").Scan(&n)
	return n, err
}

// SaveOrder stores a production order.
func (s *MSSQL) SaveOrder(ctx context.Context, o model.Order) error {
	_, err := s.db.ExecContext(ctx, "INSERT dbo.production_orders VALUES (@p1,@p2,@p3,@p4,@p5)",
		o.ID, o.Recipe, o.Status, o.Created, o.Note)
	return err
}

// SetOrderStatus updates a production order.
func (s *MSSQL) SetOrderStatus(ctx context.Context, id, status, note string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE dbo.production_orders SET status=@p2, note=@p3 WHERE id=@p1", id, status, note)
	return err
}

// SaveShipment stores a delivered pallet.
func (s *MSSQL) SaveShipment(ctx context.Context, sh model.Shipment) error {
	_, err := s.db.ExecContext(ctx, "INSERT dbo.shipments VALUES (@p1,@p2,@p3,@p4,@p5)",
		sh.PalletID, sh.Cakes, sh.Batches, sh.Vehicle, sh.At)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// InsertEvent stores an event in the traceability log.
func (s *MSSQL) InsertEvent(ctx context.Context, e *model.LogEvent) error {
	return s.db.QueryRowContext(ctx, `INSERT dbo.event_log (at,node,device,severity,code,reason,batch_id,machine_id,maintenance)
		OUTPUT INSERTED.id VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9)`,
		e.At, e.Node, nullable(e.Device), e.Severity, nullable(e.Code), trunc(e.Reason, 400),
		nullable(e.BatchID), nullable(e.MachineID), e.Maintenance).Scan(&e.ID)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Events returns the newest events first.
func (s *MSSQL) Events(ctx context.Context, limit int) ([]model.LogEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT TOP (@p1) id,at,node,ISNULL(device,''),severity,ISNULL(code,''),reason,
		ISNULL(batch_id,''),ISNULL(machine_id,''),maintenance FROM dbo.event_log ORDER BY id DESC`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.LogEvent
	for rows.Next() {
		var e model.LogEvent
		if err := rows.Scan(&e.ID, &e.At, &e.Node, &e.Device, &e.Severity, &e.Code, &e.Reason, &e.BatchID, &e.MachineID, &e.Maintenance); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DiscardCounts counts the discard events by system.
func (s *MSSQL) DiscardCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT node, COUNT(*) FROM dbo.event_log WHERE severity='discard' GROUP BY node")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var n string
		var c int
		if err := rows.Scan(&n, &c); err != nil {
			return nil, err
		}
		out[n] = c
	}
	return out, rows.Err()
}

// OpenTicket stores a maintenance ticket.
func (s *MSSQL) OpenTicket(ctx context.Context, t *model.Ticket) error {
	t.Status, t.Opened = "open", time.Now().UTC()
	return s.db.QueryRowContext(ctx, `INSERT dbo.maintenance_tickets (opened,node,machine_id,reason,status)
		OUTPUT INSERTED.id VALUES (@p1,@p2,@p3,@p4,'open')`,
		t.Opened, t.Node, t.MachineID, trunc(t.Reason, 400)).Scan(&t.ID)
}

// CloseTicket marks a ticket repaired.
func (s *MSSQL) CloseTicket(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE dbo.maintenance_tickets SET status='repaired', closed=SYSUTCDATETIME() WHERE id=@p1", id)
	return err
}

// Tickets returns the newest tickets first.
func (s *MSSQL) Tickets(ctx context.Context, limit int) ([]model.Ticket, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT TOP (@p1) id,opened,closed,node,machine_id,reason,status
		FROM dbo.maintenance_tickets ORDER BY id DESC`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Ticket
	for rows.Next() {
		var t model.Ticket
		var closed sql.NullTime
		if err := rows.Scan(&t.ID, &t.Opened, &closed, &t.Node, &t.MachineID, &t.Reason, &t.Status); err != nil {
			return nil, err
		}
		if closed.Valid {
			t.Closed = &closed.Time
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
