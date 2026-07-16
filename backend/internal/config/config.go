package config

import "github.com/lwmacct/251207-go-pkg-cfgm/pkg/cfgm"

type Config struct {
	Server Server `json:"server" desc:"AsterRouter service configuration"`
}

type Server struct {
	HTTP        HTTP        `json:"http"        desc:"HTTP service configuration"`
	Bootstrap   Bootstrap   `json:"bootstrap"   desc:"First-run bootstrap configuration"`
	Security    Security    `json:"security"    desc:"Authentication and encryption configuration"`
	Storage     Storage     `json:"storage"     desc:"Persistent storage and Redis configuration"`
	Official    Official    `json:"official"    desc:"Official catalog and license services"`
	Plugins     Plugins     `json:"plugins"     desc:"Plugin runtime configuration"`
	Jobs        Jobs        `json:"jobs"        desc:"Durable AI job infrastructure"`
	Artifacts   Artifacts   `json:"artifacts"   desc:"Artifact storage configuration"`
	Maintenance Maintenance `json:"maintenance" desc:"Backup, diagnostics, and process management"`
}

type HTTP struct {
	Listen      string `json:"listen"       desc:"HTTP listen address"`
	FrontendDir string `json:"frontend-dir" desc:"Built frontend asset directory"`
}

type Bootstrap struct {
	DeploymentRole string `json:"deployment-role" desc:"Optional unattended deployment role: personal, relay_operator, enterprise, or platform"`
	DemoMode       bool   `json:"demo-mode"       desc:"Enable the isolated demonstration login flow"`
}

type Security struct {
	Admin     Admin  `json:"admin"      desc:"Bootstrap administrator credentials"`
	SecretKey string `json:"secret-key" desc:"Stable application encryption and signing secret"`
}

type Admin struct {
	Username string `json:"username" desc:"Bootstrap administrator username"`
	Password string `json:"password" desc:"Bootstrap administrator password"`
	Token    string `json:"token"    desc:"Legacy administrator bearer token"`
}

type Storage struct {
	DatabaseURL string `json:"database-url" desc:"PostgreSQL connection URL; empty uses in-memory storage for source development"`
	Redis       Redis  `json:"redis"        desc:"Shared Redis configuration"`
}

type Redis struct {
	URL       string `json:"url"       desc:"Redis connection URL"`
	Namespace string `json:"namespace" desc:"Redis key namespace"`
}

type Official struct {
	UpdateManifestURL string          `json:"update-manifest-url" desc:"Release update manifest URL"`
	Catalog           OfficialCatalog `json:"catalog"             desc:"Official plugin catalog configuration"`
	License           OfficialLicense `json:"license"             desc:"Official license service configuration"`
	Instance          Instance        `json:"instance"            desc:"Stable installation identity"`
}

type OfficialCatalog struct {
	Mode         string `json:"mode"          desc:"Catalog mode: disabled, online, private_mirror, or offline"`
	BootstrapURL string `json:"bootstrap-url" desc:"Signed catalog bootstrap URL"`
	URL          string `json:"url"           desc:"Catalog index URL"`
	ServicesURL  string `json:"services-url"  desc:"Official services base URL"`
	KeyID        string `json:"key-id"        desc:"Catalog signature key ID"`
	PublicKey    string `json:"public-key"    desc:"Catalog signature public key"`
}

type OfficialLicense struct {
	URL       string `json:"url"        desc:"License service URL"`
	RedeemURL string `json:"redeem-url" desc:"License redemption URL"`
	KeyID     string `json:"key-id"     desc:"License signature key ID"`
	PublicKey string `json:"public-key" desc:"License signature public key"`
}

type Instance struct {
	ID          string `json:"id"          desc:"Stable installation ID"`
	Fingerprint string `json:"fingerprint" desc:"Installation fingerprint"`
	DisplayName string `json:"display-name" desc:"Installation display name"`
}

type Plugins struct {
	CacheDir  string `json:"cache-dir"  desc:"Downloaded plugin package cache"`
	ActiveDir string `json:"active-dir" desc:"Activated plugin runtime directory; derived from cache-dir when empty"`
	HostURL   string `json:"host-url"   desc:"Plugin host callback URL; derived from http.listen when empty"`
}

type Jobs struct {
	Queue                 JobQueue `json:"queue"                   desc:"Durable AI job delivery queue"`
	RoutingAffinityDriver string   `json:"routing-affinity-driver" desc:"Routing affinity driver: repository or redis"`
}

type JobQueue struct {
	Driver string    `json:"driver" desc:"Queue driver: memory or redis"`
	Limits JobLimits `json:"limits" desc:"Concurrent queued job admission limits; zero disables a limit"`
}

type JobLimits struct {
	Profile   int `json:"profile"   desc:"Maximum queued jobs per profile"`
	Tenant    int `json:"tenant"    desc:"Maximum queued jobs per tenant"`
	Principal int `json:"principal" desc:"Maximum queued jobs per principal"`
}

type Artifacts struct {
	Driver    string     `json:"driver"     desc:"Artifact store driver: none, local, or s3"`
	LocalRoot string     `json:"local-root" desc:"Local artifact root directory"`
	S3        ArtifactS3 `json:"s3"         desc:"S3-compatible artifact store"`
}

type ArtifactS3 struct {
	Endpoint  string `json:"endpoint"   desc:"S3-compatible endpoint URL"`
	Region    string `json:"region"     desc:"S3 region"`
	Bucket    string `json:"bucket"     desc:"S3 bucket"`
	Prefix    string `json:"prefix"     desc:"S3 object key prefix"`
	AccessKey string `json:"access-key" desc:"S3 access key"`
	SecretKey string `json:"secret-key" desc:"S3 secret key"`
	PathStyle bool   `json:"path-style" desc:"Use path-style S3 addressing"`
}

type Maintenance struct {
	BackupDir       string `json:"backup-dir"        desc:"Local backup directory"`
	DiagnosticDir   string `json:"diagnostic-dir"    desc:"Diagnostic bundle directory"`
	MaxArchiveBytes int64  `json:"max-archive-bytes" desc:"Maximum backup or diagnostic archive size"`
	AllowRestart    bool   `json:"allow-restart"     desc:"Allow managed process restart operations"`
}

func DefaultConfig() Config {
	return Config{Server: Server{
		HTTP:     HTTP{Listen: ":8080", FrontendDir: "../frontend/dist"},
		Security: Security{Admin: Admin{Username: "admin"}},
		Storage:  Storage{Redis: Redis{Namespace: "asterrouter"}},
		Official: Official{Catalog: OfficialCatalog{Mode: "disabled"}},
		Plugins:  Plugins{CacheDir: "data/plugin-cache"},
		Jobs: Jobs{
			Queue:                 JobQueue{Driver: "memory"},
			RoutingAffinityDriver: "repository",
		},
		Artifacts: Artifacts{
			Driver:    "none",
			LocalRoot: "data/artifacts",
			S3:        ArtifactS3{Region: "auto"},
		},
		Maintenance: Maintenance{
			BackupDir:       "data/backups",
			DiagnosticDir:   "data/diagnostics",
			MaxArchiveBytes: 2 << 30,
		},
	}}
}

var Manager = cfgm.New(
	DefaultConfig(),
	cfgm.AppName("asterrouter"),
)
