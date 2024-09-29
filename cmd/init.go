/*
Copyright © 2024 Mahmoud Mosua <m.mousa@hey.com>

Licensed under the GNU GPL License, Version 3.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
https://www.gnu.org/licenses/gpl-3.0.en.html

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mightymoud/sidekick/render"
	"github.com/mightymoud/sidekick/utils"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Init sidekick CLI and configure your VPS to host your apps",
	Long: `This command will run you through the setup steps to get sidekick loaded on your VPS.
		You wil need to provide your VPS IPv4 address and a registry to host your docker images.
		`,
	Run: func(cmd *cobra.Command, args []string) {
		pterm.DefaultBasicText.Println("Welcome to Sidekick. We need to collect some details from you first")

		render.RenderSidekickBig()
		server := viper.GetString("serverAddress")
		certEmail := viper.GetString("certEmail")

		if server == "" {
			serverTextInput := pterm.DefaultInteractiveTextInput
			serverTextInput.DefaultText = "Please enter the IPv4 Address of your VPS"
			server, _ = serverTextInput.Show()
			if !utils.IsValidIPAddress(server) {
				pterm.Error.Printfln("You entered an incorrect IP Address - %s", server)
				os.Exit(0)
			}
		}

		if certEmail == "" {
			certEmailTextInput := pterm.DefaultInteractiveTextInput
			certEmailTextInput.DefaultText = "Please enter an email for use with TLS certs"
			certEmail, _ = certEmailTextInput.Show()
			if certEmail == "" {
				pterm.Error.Println("An email is needed before you proceed")
				os.Exit(0)
			}
		}

		viper.Set("serverAddress", server)
		viper.Set("certEmail", certEmail)

		pterm.Println()
		pterm.DefaultHeader.WithFullWidth().Println("Sidekick booting up! 🚀")
		pterm.Println()

		// init login with checking handshake
		rootSshClient, err := utils.Login(server, "root")
		if err != nil {
			log.Fatalf("Unable to login using 'root' user: %s", err)
			os.Exit(1)
		}

		multi := pterm.DefaultMultiPrinter
		rootLoginSpinner, _ := pterm.DefaultSpinner.Start("Logging in with root")
		stage0Spinner, _ := utils.GetSpinner().WithWriter(multi.NewWriter()).Start("Adding user Sidekick")
		sidekickLoginSpinner, _ := utils.GetSpinner().WithWriter(multi.NewWriter()).Start("Logging into with sidekick user")
		stage1Spinner, _ := utils.GetSpinner().WithWriter(multi.NewWriter()).Start("Setting up VPS")
		stage2Spinner, _ := utils.GetSpinner().WithWriter(multi.NewWriter()).Start("Setting up Docker")
		stage3Spinner, _ := utils.GetSpinner().WithWriter(multi.NewWriter()).Start("Setting up Traefik")
		pterm.Println()
		multi.Start()

		rootLoginSpinner.Success("Logged in successfully!")

		stage0Spinner.Sequence = []string{"▀ ", " ▀", " ▄", "▄ "}
		if err := utils.RunStage(rootSshClient, utils.UsersetupStage); err != nil {
			stage0Spinner.Fail(utils.UsersetupStage.SpinnerFailMessage)
			panic(err)
		}
		stage0Spinner.Success(utils.UsersetupStage.SpinnerSuccessMessage)

		sidekickLoginSpinner.Sequence = []string{"▀ ", " ▀", " ▄", "▄ "}
		sidekickSshClient, err := utils.Login(server, "sidekick")
		if err != nil {
			sidekickLoginSpinner.Fail("Something went wrong logging in to your VPS")
			panic(err)
		}
		sidekickLoginSpinner.Success("Logged in successfully with new user!")

		stage1Spinner.Sequence = []string{"▀ ", " ▀", " ▄", "▄ "}
		if err := utils.RunStage(sidekickSshClient, utils.SetupStage); err != nil {
			stage1Spinner.Fail(utils.SetupStage.SpinnerFailMessage)
			panic(err)
		}
		ch, sessionErr := utils.RunCommand(sidekickSshClient, "mkdir -p $HOME/.config/sops/age/ && age-keygen -o $HOME/.config/sops/age/keys.txt 2>&1 ")
		if sessionErr != nil {
			stage1Spinner.Fail(utils.SetupStage.SpinnerFailMessage)
			panic(sessionErr)
		}
		select {
		case output := <-ch:
			if strings.HasPrefix(output, "Public key") {
				publicKey := strings.Split(output, " ")[2:3]
				viper.Set("publicKey", publicKey[0])
			}
		}
		stage1Spinner.Success(utils.SetupStage.SpinnerSuccessMessage)

		stage2Spinner.Sequence = []string{"▀ ", " ▀", " ▄", "▄ "}
		if err := utils.RunStage(sidekickSshClient, utils.DockerStage); err != nil {
			stage2Spinner.Fail(utils.DockerStage.SpinnerFailMessage)
			panic(err)
		}
		stage2Spinner.Success(utils.DockerStage.SpinnerSuccessMessage)

		stage3Spinner.Sequence = []string{"▀ ", " ▀", " ▄", "▄ "}
		traefikStage := utils.GetTraefikStage(certEmail)
		if err := utils.RunStage(sidekickSshClient, traefikStage); err != nil {
			stage3Spinner.Fail(traefikStage.SpinnerFailMessage)
			panic(err)
		}
		stage3Spinner.Success(traefikStage.SpinnerSuccessMessage)

		if err := viper.WriteConfig(); err != nil {
			panic(err)
		}

		multi.Stop()

		pterm.Println()
		pterm.Info.Println("Your VPS is ready! You can now run Sidekick launch in your app folder")
		pterm.Println()
	},
}

func initConfig() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	configPath := fmt.Sprintf("%s/.config/sidekick", home)
	configFile := fmt.Sprintf("%s/default.yaml", configPath)

	makeDirErr := os.MkdirAll(configPath, os.ModePerm)
	if makeDirErr != nil {
		log.Fatalf("Error creating directory: %v\n", makeDirErr)
		os.Exit(1)
	}

	viper.AddConfigPath(configPath)
	viper.SetConfigType("yaml")
	viper.SetConfigName("default")
	file, fileCreateErr := os.Create(configFile)
	if fileCreateErr != nil {
		log.Fatalf("Error creating configFile: %v\n", fileCreateErr)
		os.Exit(1)

	}
	file.Close()

}

func init() {
	rootCmd.AddCommand(initCmd)
	cobra.OnInitialize(initConfig)

	initCmd.Flags().StringP("server", "s", "", "Set the IP address of your Server")
	viper.BindPFlag("serverAddress", initCmd.Flags().Lookup("server"))

	initCmd.Flags().StringP("email", "e", "", "An email address to be used for SSL certs")
	viper.BindPFlag("certEmail", initCmd.Flags().Lookup("email"))
}
