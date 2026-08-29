package db

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens a GORM connection to MySQL and verifies it with a ping.
//
// TAHAN boot-race: seluruh gorm.Open + ping di-RETRY sebagai satu kesatuan.
// Ini penting karena saat MySQL belum menyala (mis. host baru reboot dan semua
// container start berbarengan), gorm.Open SENDIRI gagal "connection refused"
// dan langsung return — bukan hanya ping-nya. depends_on:service_healthy hanya
// mengatur urutan saat `docker compose up`, TIDAK berlaku pada auto-restart
// setelah reboot; retry di sini yang membuat backend selalu pulih sendiri.
func Connect(dsn string, debug bool) (*gorm.DB, error) {
	logLevel := logger.Silent
	if debug {
		logLevel = logger.Info
	}

	// MySQL cold start (inisialisasi pertama) bisa 30–60 dtk; beri tenggat longgar.
	const maxWait = 90 * time.Second
	const retryInterval = 2 * time.Second
	deadline := time.Now().Add(maxWait)

	for attempt := 1; ; attempt++ {
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logLevel),
		})
		if err == nil {
			sqlDB, derr := db.DB()
			if derr != nil {
				return nil, fmt.Errorf("db.DB(): %w", derr)
			}
			if perr := sqlDB.Ping(); perr == nil {
				// Connection pool settings — tune for production load.
				sqlDB.SetMaxOpenConns(25)
				sqlDB.SetMaxIdleConns(10)
				sqlDB.SetConnMaxLifetime(5 * time.Minute)
				if attempt > 1 {
					log.Printf("db: MySQL siap setelah %d percobaan.", attempt)
				}
				return db, nil
			} else {
				err = perr
			}
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("db: MySQL tidak siap setelah %s: %w", maxWait, err)
		}
		log.Printf("db: MySQL belum siap (%v) — coba lagi dalam %s...", err, retryInterval)
		time.Sleep(retryInterval)
	}
}
