package main

import (
	"./client"
	"./fileio"
	"./game"
	"./render"
	"fmt"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"log"
	"runtime"
)

const (
	WINDOW_WIDTH  = 1024
	WINDOW_HEIGHT = 768
)

var (
	windowHandler *client.WindowHandler
	gameDef       *game.GameDef
)

func handleInput(player *game.Player, collisionEntities []fileio.CollisionEntity) {
	if windowHandler.InputHandler.IsActive(client.PLAYER_FORWARD) {
		predictPosition := gameDef.PredictPositionForward(player.Position, player.RotationAngle)
		collidingEntity := gameDef.CheckCollision(predictPosition, collisionEntities)
		if collidingEntity == nil {
			player.Position = predictPosition
			player.PoseNumber = 0
		} else {
			if gameDef.CheckRamp(collidingEntity) {
				predictPosition := gameDef.PredictPositionForwardSlope(player.Position, player.RotationAngle, collidingEntity)
				player.Position = predictPosition
				player.PoseNumber = 1
			} else {
				player.PoseNumber = -1
			}
		}
	}

	if windowHandler.InputHandler.IsActive(client.PLAYER_BACKWARD) {
		predictPosition := gameDef.PredictPositionBackward(player.Position, player.RotationAngle)
		collidingEntity := gameDef.CheckCollision(predictPosition, collisionEntities)
		if collidingEntity == nil {
			player.Position = predictPosition
			player.PoseNumber = 1
		} else {
			if gameDef.CheckRamp(collidingEntity) {
				predictPosition := gameDef.PredictPositionBackwardSlope(player.Position, player.RotationAngle, collidingEntity)
				player.Position = predictPosition
				player.PoseNumber = 1
			} else {
				player.PoseNumber = -1
			}
		}
	}

	if !windowHandler.InputHandler.IsActive(client.PLAYER_FORWARD) &&
		!windowHandler.InputHandler.IsActive(client.PLAYER_BACKWARD) {
		player.PoseNumber = -1
	}

	if windowHandler.InputHandler.IsActive(client.PLAYER_ROTATE_LEFT) {
		player.RotationAngle -= 5
		if player.RotationAngle < 0 {
			player.RotationAngle += 360
		}
	}

	if windowHandler.InputHandler.IsActive(client.PLAYER_ROTATE_RIGHT) {
		player.RotationAngle += 5
		if player.RotationAngle > 360 {
			player.RotationAngle -= 360
		}
	}
}

func main() {
	// Run OpenGL code
	runtime.LockOSThread()
	if err := glfw.Init(); err != nil {
		panic(fmt.Errorf("Could not initialize glfw: %v", err))
	}
	defer glfw.Terminate()
	windowHandler = client.NewWindowHandler(WINDOW_WIDTH, WINDOW_HEIGHT, "OpenBiohazard2")

	renderDef := render.InitRenderer(WINDOW_WIDTH, WINDOW_HEIGHT)

	roomcutBinFilename := game.ROOMCUT_FILE
	roomcutBinOutput := fileio.LoadBINFile(roomcutBinFilename)

	// Load player model
	pldOutput, err := fileio.LoadPLDFile(game.LEON_MODEL_FILE)
	if err != nil {
		log.Fatal(err)
	}
	modelTexColors := pldOutput.TextureData.ConvertToRenderData()
	playerTextureId := render.BuildTexture(modelTexColors,
		int32(pldOutput.TextureData.ImageWidth), int32(pldOutput.TextureData.ImageHeight))
	playerEntityVertexBuffer := render.BuildEntityComponentVertices(pldOutput)

	gameDef = game.NewGame(1, 0, 0)
	gameDef.Player = game.NewPlayer(mgl32.Vec3{18781, 0, -2664}, 180)

	var roomOutput *fileio.RoomImageOutput
	var cameraPositionData []fileio.CameraInfo
	var cameraSwitches []fileio.RVDHeader
	var cameraSwitchTransitions map[int][]int
	var cameraMaskData [][]fileio.MaskRectangle
	var collisionEntities []fileio.CollisionEntity
	var lightData []fileio.LITCameraLight
	var initScriptData [][][]byte

	for !windowHandler.ShouldClose() {
		windowHandler.StartFrame()

		if !gameDef.IsRoomLoaded {
			roomFilename := gameDef.GetRoomFilename(game.PLAYER_LEON)
			rdtOutput, err := fileio.LoadRDTFile(roomFilename)
			if err != nil {
				log.Fatal("Error loading RDT file. ", err)
			}
			gameDef.MaxCamerasInRoom = int(rdtOutput.Header.NumCameras)
			fmt.Println("Loaded", roomFilename, ". Max cameras in room = ", gameDef.MaxCamerasInRoom)
			cameraSwitches = rdtOutput.CameraSwitchData.CameraSwitches
			cameraSwitchTransitions = gameDef.GenerateCameraSwitchTransitions(cameraSwitches)
			cameraPositionData = rdtOutput.RIDOutput.CameraPositions
			cameraMaskData = rdtOutput.RIDOutput.CameraMasks
			collisionEntities = rdtOutput.CollisionData.CollisionEntities
			lightData = rdtOutput.LightData.Lights
			initScriptData = rdtOutput.InitScriptData.ScriptData
			gameDef.RunScript(initScriptData)

			gameDef.IsRoomLoaded = true
		}

		if !gameDef.IsCameraLoaded {
			// Update camera position
			cameraPosition := cameraPositionData[gameDef.CameraId]
			renderDef.Camera.CameraFrom = cameraPosition.CameraFrom
			renderDef.Camera.CameraTo = cameraPosition.CameraTo
			renderDef.ViewMatrix = renderDef.Camera.GetViewMatrix()
			renderDef.SetEnvironmentLight(lightData[gameDef.CameraId])

			backgroundImageNumber := gameDef.GetBackgroundImageNumber()
			roomOutput = fileio.ExtractRoomBackground(roomcutBinFilename, roomcutBinOutput, backgroundImageNumber)

			if roomOutput.BackgroundImage != nil {
				render.GenerateBackgroundImageEntity(renderDef, roomOutput.BackgroundImage.ConvertToRenderData())
				// Camera image mask depends on updated camera position
				render.GenerateCameraImageMaskEntity(renderDef, roomOutput, cameraMaskData[gameDef.CameraId])
			}

			gameDef.IsCameraLoaded = true
		}

		timeElapsedSeconds := windowHandler.GetTimeSinceLastFrame()
		// Only render these entities for debugging
		debugEntities := render.DebugEntities{
			CameraId:                gameDef.CameraId,
			CameraSwitches:          cameraSwitches,
			CameraSwitchTransitions: cameraSwitchTransitions,
			CollisionEntities:       collisionEntities,
			Doors:                   gameDef.Doors,
		}
		// Update screen
		playerEntity := render.PlayerEntity{
			TextureId:           playerTextureId,
			VertexBuffer:        playerEntityVertexBuffer,
			PLDOutput:           pldOutput,
			Player:              gameDef.Player,
			AnimationPoseNumber: gameDef.Player.PoseNumber,
		}
		renderDef.RenderFrame(playerEntity, debugEntities, timeElapsedSeconds)

		handleInput(gameDef.Player, collisionEntities)
		gameDef.HandleCameraSwitch(gameDef.Player.Position, cameraSwitches, cameraSwitchTransitions)
		gameDef.HandleRoomSwitch(gameDef.Player.Position)
	}
}
