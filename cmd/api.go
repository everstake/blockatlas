package cmd

import (
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/trustwallet/blockatlas/api"
	_ "github.com/trustwallet/blockatlas/docs"
	"github.com/trustwallet/blockatlas/pkg/ginutils"
	"github.com/trustwallet/blockatlas/pkg/logger"
)

var apiCmd = &cobra.Command{
	Use:   "api <bind>",
	Short: "API server",
	Args:  cobra.MaximumNArgs(1),
	Run:   runApi,
}

func runApi(_ *cobra.Command, args []string) {
	var bind string
	if len(args) == 0 {
		bind = ":8420"
	} else {
		bind = args[0]
	}
	RunApi(bind, nil)
}

func RunApi(bind string, c chan *gin.Engine) {
	gin.SetMode(viper.GetString("gin.mode"))
	engine := gin.Default()

	sg := sentrygin.New(sentrygin.Options{})
	engine.Use(ginutils.CheckReverseProxy, sg)

	engine.Use(ginutils.CORSMiddleware())
	engine.OPTIONS("/*path", ginutils.CORSMiddleware())

	engine.GET("swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	engine.GET("/", api.GetRoot)
	engine.GET("/status", func(c *gin.Context) {
		ginutils.RenderSuccess(c, map[string]interface{}{
			"status": true,
		})
	})

	api.MakeMetricsRoute(engine)
	api.LoadPlatforms(engine)

	if viper.GetBool("observer.enabled") {
		logger.Info("Loading observer API")
		observerAPI := engine.Group("/observer/v1")
		api.SetupObserverAPI(observerAPI, Storage)
	}

	if viper.GetBool("market.enabled") {
		logger.Info("Loading market API")
		marketAPI := engine.Group("/v1/market")
		api.SetupMarketAPI(marketAPI, Storage)
	}

	if c != nil {
		c <- engine
	}

	logger.Info("Running application", logger.Params{"bind": bind})
	if err := engine.Run(bind); err != nil {
		logger.Fatal("Application failed", err)
	}
}

func init() {
	rootCmd.AddCommand(apiCmd)
}
