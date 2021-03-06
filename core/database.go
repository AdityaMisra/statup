// Statup
// Copyright (C) 2018.  Hunter Long and the project contributors
// Written by Hunter Long <info@socialeck.com> and the project contributors
//
// https://github.com/hunterlong/statup
//
// The licenses for most software and other practical works are designed
// to take away your freedom to share and change the works.  By contrast,
// the GNU General Public License is intended to guarantee your freedom to
// share and change all versions of a program--to make sure it remains free
// software for all its users.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"github.com/go-yaml/yaml"
	"github.com/hunterlong/statup/core/notifier"
	"github.com/hunterlong/statup/types"
	"github.com/hunterlong/statup/utils"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mssql"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"os"
	"time"
)

var (
	DbSession *gorm.DB
)

func (s *Service) allHits() *gorm.DB {
	var hits []*Hit
	return servicesDB().Find(s).Related(&hits)
}

// failuresDB returns the 'failures' database column
func failuresDB() *gorm.DB {
	return DbSession.Model(&types.Failure{})
}

// hitsDB returns the 'hits' database column
func hitsDB() *gorm.DB {
	return DbSession.Model(&types.Hit{})
}

// servicesDB returns the 'services' database column
func servicesDB() *gorm.DB {
	return DbSession.Model(&types.Service{})
}

// coreDB returns the single column 'core'
func coreDB() *gorm.DB {
	return DbSession.Table("core").Model(&CoreApp)
}

// usersDB returns the 'users' database column
func usersDB() *gorm.DB {
	return DbSession.Model(&types.User{})
}

// commDB returns the 'communications' database column
func commDB() *gorm.DB {
	return DbSession.Model(&notifier.Notification{})
}

// hitsDB returns the 'hits' database column
func checkinDB() *gorm.DB {
	return DbSession.Model(&types.Checkin{})
}

type DbConfig struct {
	*types.DbConfig
}

// Close shutsdown the database connection
func (db *DbConfig) Close() error {
	return DbSession.DB().Close()
}

func (s *Service) AfterFind() (err error) {
	s.CreatedAt = utils.Timezoner(s.CreatedAt, CoreApp.Timezone)
	return
}

func (s *Hit) AfterFind() (err error) {
	s.CreatedAt = utils.Timezoner(s.CreatedAt, CoreApp.Timezone)
	return
}

func (f *Failure) AfterFind() (err error) {
	f.CreatedAt = utils.Timezoner(f.CreatedAt, CoreApp.Timezone)
	return
}

func (u *User) AfterFind() (err error) {
	u.CreatedAt = utils.Timezoner(u.CreatedAt, CoreApp.Timezone)
	return
}

func (u *Hit) BeforeCreate() (err error) {
	u.CreatedAt = time.Now().UTC()
	return
}

func (u *Failure) BeforeCreate() (err error) {
	u.CreatedAt = time.Now().UTC()
	return
}

func (u *User) BeforeCreate() (err error) {
	u.CreatedAt = time.Now().UTC()
	return
}

func (u *Service) BeforeCreate() (err error) {
	u.CreatedAt = time.Now().UTC()
	return
}

// InsertCore create the single row for the Core settings in Statup
func (db *DbConfig) InsertCore() (*Core, error) {
	CoreApp = &Core{Core: &types.Core{
		Name:        db.Project,
		Description: db.Description,
		Config:      "config.yml",
		ApiKey:      utils.NewSHA1Hash(9),
		ApiSecret:   utils.NewSHA1Hash(16),
		Domain:      db.Domain,
		MigrationId: time.Now().Unix(),
	}}
	CoreApp.DbConnection = db.DbConn
	query := coreDB().Create(&CoreApp)
	return CoreApp, query.Error
}

// Connect will attempt to connect to the sqlite, postgres, or mysql database
func (db *DbConfig) Connect(retry bool, location string) error {
	var err error
	if DbSession != nil {
		DbSession = nil
	}
	var conn, dbType string
	dbType = Configs.DbConn
	switch dbType {
	case "sqlite":
		conn = utils.Directory + "/statup.db"
		dbType = "sqlite3"
	case "mysql":
		if Configs.DbPort == 0 {
			Configs.DbPort = 3306
		}
		host := fmt.Sprintf("%v:%v", Configs.DbHost, Configs.DbPort)
		conn = fmt.Sprintf("%v:%v@tcp(%v)/%v?charset=utf8&parseTime=True&loc=UTC", Configs.DbUser, Configs.DbPass, host, Configs.DbData)
	case "postgres":
		if Configs.DbPort == 0 {
			Configs.DbPort = 5432
		}
		conn = fmt.Sprintf("host=%v port=%v user=%v dbname=%v password=%v sslmode=disable", Configs.DbHost, Configs.DbPort, Configs.DbUser, Configs.DbData, Configs.DbPass)
	case "mssql":
		if Configs.DbPort == 0 {
			Configs.DbPort = 1433
		}
		host := fmt.Sprintf("%v:%v", Configs.DbHost, Configs.DbPort)
		conn = fmt.Sprintf("sqlserver://%v:%v@%v?database=%v", Configs.DbUser, Configs.DbPass, host, Configs.DbData)
	}
	DbSession, err = gorm.Open(dbType, conn)
	if err != nil {
		if retry {
			utils.Log(1, fmt.Sprintf("Database connection to '%v' is not available, trying again in 5 seconds...", Configs.DbHost))
			return db.waitForDb()
		} else {
			fmt.Println("ERROR:", err)
			return err
		}
	}
	err = DbSession.DB().Ping()
	if err == nil {
		utils.Log(1, fmt.Sprintf("Database connection to '%v' was successful.", Configs.DbData))
	}
	return err
}

func (db *DbConfig) waitForDb() error {
	time.Sleep(5 * time.Second)
	return db.Connect(true, utils.Directory)
}

// DatabaseMaintence will automatically delete old records from 'failures' and 'hits'
// this function is currently set to delete records 7+ days old every 60 minutes
func DatabaseMaintence() {
	for range time.Tick(60 * time.Minute) {
		utils.Log(1, "Checking for database records older than 7 days...")
		since := time.Now().AddDate(0, 0, -7)
		DeleteAllSince("failures", since)
		DeleteAllSince("hits", since)
	}
}

// DeleteAllSince will delete a specific table's records based on a time.
func DeleteAllSince(table string, date time.Time) {
	sql := fmt.Sprintf("DELETE FROM %v WHERE created_at < '%v';", table, date.Format("2006-01-02"))
	db := DbSession.Raw(sql)
	if db.Error != nil {
		utils.Log(2, db.Error)
	}
}

// Update will save the config.yml file
func (c *DbConfig) Update() error {
	var err error
	config, err := os.Create(utils.Directory + "/config.yml")
	if err != nil {
		utils.Log(4, err)
		return err
	}
	data, err := yaml.Marshal(c.DbConfig)
	if err != nil {
		utils.Log(3, err)
		return err
	}
	config.WriteString(string(data))
	config.Close()
	return err
}

// Save will initially create the config.yml file
func (c *DbConfig) Save() (*DbConfig, error) {
	var err error
	config, err := os.Create(utils.Directory + "/config.yml")
	if err != nil {
		utils.Log(4, err)
		return nil, err
	}
	c.ApiKey = utils.NewSHA1Hash(16)
	c.ApiSecret = utils.NewSHA1Hash(16)
	data, err := yaml.Marshal(c.DbConfig)
	if err != nil {
		utils.Log(3, err)
		return nil, err
	}
	config.WriteString(string(data))
	defer config.Close()
	return c, err
}

// CreateCore will initialize the global variable 'CoreApp". This global variable contains most of Statup app.
func (c *DbConfig) CreateCore() *Core {
	newCore := &types.Core{
		Name:        c.Project,
		Description: c.Description,
		Config:      "config.yml",
		ApiKey:      c.ApiKey,
		ApiSecret:   c.ApiSecret,
		Domain:      c.Domain,
		MigrationId: time.Now().Unix(),
	}
	db := coreDB().Create(&newCore)
	if db.Error == nil {
		CoreApp = &Core{Core: newCore}
	}
	CoreApp, err := SelectCore()
	if err != nil {
		utils.Log(4, err)
	}
	return CoreApp
}

// SeedDatabase will insert many elements into the database. This is only ran in Dev/Test move
//func (db *DbConfig) SeedDatabase() (string, string, error) {
//	utils.Log(1, "Seeding Database with Dummy Data...")
//	dir := utils.Directory
//	var cmd string
//	switch db.DbConn {
//	case "sqlite":
//		cmd = fmt.Sprintf("cat %v/dev/sqlite_seed.sql | sqlite3 %v/statup.db", dir, dir)
//	case "mysql":
//		cmd = fmt.Sprintf("mysql -h %v -P %v -u %v --password=%v %v < %v/dev/mysql_seed.sql", Configs.DbHost, 3306, Configs.DbUser, Configs.DbPass, Configs.DbData, dir)
//	case "postgres":
//		cmd = fmt.Sprintf("PGPASSWORD=%v psql -U %v -h %v -d %v -1 -f %v/dev/postgres_seed.sql", db.DbPass, db.DbUser, db.DbHost, db.DbData, dir)
//	}
//	out, outErr, err := utils.Command(cmd)
//	return out, outErr, err
//}

// DropDatabase will DROP each table Statup created
func (db *DbConfig) DropDatabase() error {
	utils.Log(1, "Dropping Database Tables...")
	err := DbSession.DropTableIfExists("checkins")
	err = DbSession.DropTableIfExists("notifications")
	err = DbSession.DropTableIfExists("core")
	err = DbSession.DropTableIfExists("failures")
	err = DbSession.DropTableIfExists("hits")
	err = DbSession.DropTableIfExists("services")
	err = DbSession.DropTableIfExists("users")
	return err.Error
}

// CreateDatabase will CREATE TABLES for each of the Statup elements
func (db *DbConfig) CreateDatabase() error {
	utils.Log(1, "Creating Database Tables...")
	err := DbSession.CreateTable(&types.Checkin{})
	err = DbSession.CreateTable(&notifier.Notification{})
	err = DbSession.Table("core").CreateTable(&types.Core{})
	err = DbSession.CreateTable(&types.Failure{})
	err = DbSession.CreateTable(&types.Hit{})
	err = DbSession.CreateTable(&types.Service{})
	err = DbSession.CreateTable(&types.User{})
	utils.Log(1, "Statup Database Created")
	return err.Error
}

// MigrateDatabase will migrate the database structure to current version.
// This function will NOT remove previous records, tables or columns from the database.
// If this function has an issue, it will ROLLBACK to the previous state.
func (db *DbConfig) MigrateDatabase() error {
	utils.Log(1, "Migrating Database Tables...")

	tx := DbSession.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()
	if tx.Error != nil {
		return tx.Error
	}
	tx = tx.AutoMigrate(&types.Service{}, &types.User{}, &types.Hit{}, &types.Failure{}, &types.Checkin{}, &notifier.Notification{}).Table("core").AutoMigrate(&types.Core{})
	if tx.Error != nil {
		tx.Rollback()
		utils.Log(3, fmt.Sprintf("Statup Database could not be migrated: %v", tx.Error))
		return tx.Error
	}
	utils.Log(1, "Statup Database Migrated")
	return tx.Commit().Error
}

func (c *DbConfig) Clean() *DbConfig {
	if os.Getenv("DB_PORT") != "" {
		if c.DbConn == "postgres" {
			c.DbHost = c.DbHost + ":" + os.Getenv("DB_PORT")
		}
	}
	return c
}
