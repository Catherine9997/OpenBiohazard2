package main

import (
	"fmt"
	"log"

	"github.com/samuelyuan/openbiohazard2/client"
	"github.com/samuelyuan/openbiohazard2/fileio"
	"github.com/samuelyuan/openbiohazard2/game"
	"github.com/samuelyuan/openbiohazard2/render"
	"github.com/samuelyuan/openbiohazard2/script"
)

type MainGameStateInput struct {
	GameDef        *game.GameDef
	ScriptDef      *script.ScriptDef
	MainGameRender *MainGameRender
}

type MainGameRender struct {
	RenderDef               *render.RenderDef
	RoomcutBinOutput        *fileio.BinOutput
	RenderRoom              render.RenderRoom
	PlayerEntity            *render.PlayerEntity
	DebugEntities           []*render.DebugEntity
	CameraSwitchDebugEntity *render.DebugEntity
}

func NewMainGameStateInput(renderDef *render.RenderDef, gameDef *game.GameDef) *MainGameStateInput {
	return &MainGameStateInput{
		GameDef:        gameDef,
		ScriptDef:      script.NewScriptDef(),
		MainGameRender: NewMainGameRender(renderDef),
	}
}

func NewMainGameRender(renderDef *render.RenderDef) *MainGameRender {
	// Load player model
	pldOutput, err := fileio.LoadPLDFile(game.LEON_MODEL_FILE)
	if err != nil {
		log.Fatal("Error loading player model: ", err)
	}

	// Core sprite file has sprite ids 0-7
	// All other sprites are loaded based on the room
	fileio.LoadESPFile(game.CORE_SPRITE_FILE)

	return &MainGameRender{
		RenderDef:               renderDef,
		RoomcutBinOutput:        fileio.LoadBINFile(game.ROOMCUT_FILE),
		PlayerEntity:            render.NewPlayerEntity(pldOutput),
		DebugEntities:           make([]*render.DebugEntity, 0),
		CameraSwitchDebugEntity: nil,
	}
}

func handleMainGame(mainGameStateInput *MainGameStateInput, gameStateManager *GameStateManager) {
	gameDef := mainGameStateInput.GameDef

	switch gameDef.StateStatus {
	case game.GAME_LOAD_ROOM:
		loadRoomState(mainGameStateInput)
		gameDef.StateStatus = game.GAME_LOAD_CAMERA
	case game.GAME_LOAD_CAMERA:
		loadCameraState(mainGameStateInput)
		gameDef.StateStatus = game.GAME_LOOP
	case game.GAME_LOOP:
		runGameLoop(mainGameStateInput, gameStateManager)
	}
}

func loadRoomState(mainGameStateInput *MainGameStateInput) {
	gameDef := mainGameStateInput.GameDef
	scriptDef := mainGameStateInput.ScriptDef
	mainGameRender := mainGameStateInput.MainGameRender
	renderDef := mainGameRender.RenderDef

	// Load room data from file
	roomFilename := gameDef.GetRoomFilename(game.PLAYER_LEON)
	rdtOutput, err := fileio.LoadRDTFile(roomFilename)
	if err != nil {
		log.Fatal("Error loading RDT file. ", err)
	}
	fmt.Println("Loaded", roomFilename)
	gameDef.MaxCamerasInRoom = int(rdtOutput.Header.NumCameras)
	fmt.Println("Max cameras in room = ", gameDef.MaxCamerasInRoom)
	gameDef.GameRoom = gameDef.NewGameRoom(rdtOutput)
	mainGameRender.RenderRoom = render.NewRenderRoom(rdtOutput)

	// Initialize room model objects
	renderDef.ItemGroupEntity.ItemTextureData = mainGameRender.RenderRoom.ItemTextureData
	renderDef.ItemGroupEntity.ItemModelData = mainGameRender.RenderRoom.ItemModelData

	// Initialize sprite textures
	renderDef.SpriteGroupEntity = render.NewSpriteGroupEntity(mainGameRender.RenderRoom.SpriteData)

	// Initialize scripts
	scriptDef.Reset()

	// Run initial script once when the room loads
	threadNum := 0
	functionNum := 0
	scriptDef.InitScript(gameDef.GameRoom.InitScriptData, threadNum, functionNum)
	scriptDef.RunScript(gameDef.GameRoom.InitScriptData, 10.0, gameDef, renderDef)

	// Run the room script in the game loop
	threadNum = 0
	functionNum = 0
	scriptDef.InitScript(gameDef.GameRoom.RoomScriptData, threadNum, functionNum)
	threadNum = 1
	functionNum = 1
	scriptDef.InitScript(gameDef.GameRoom.RoomScriptData, threadNum, functionNum)

	mainGameRender.DebugEntities = render.BuildAllDebugEntities(gameDef)
}

func loadCameraState(mainGameStateInput *MainGameStateInput) {
	gameDef := mainGameStateInput.GameDef
	mainGameRender := mainGameStateInput.MainGameRender
	renderDef := mainGameRender.RenderDef
	roomcutBinOutput := mainGameRender.RoomcutBinOutput

	// Update camera position
	cameraPosition := gameDef.GameRoom.CameraPositionData[gameDef.CameraId]
	renderDef.Camera.CameraFrom = cameraPosition.CameraFrom
	renderDef.Camera.CameraTo = cameraPosition.CameraTo
	renderDef.Camera.CameraFov = cameraPosition.CameraFov
	renderDef.ViewMatrix = renderDef.Camera.BuildViewMatrix()
	renderDef.EnvironmentLight = render.BuildEnvironmentLight(mainGameRender.RenderRoom.LightData[gameDef.CameraId])

	// Update background image
	backgroundImageNumber := gameDef.GetBackgroundImageNumber()
	roomOutput := fileio.ExtractRoomBackground(game.ROOMCUT_FILE, roomcutBinOutput, backgroundImageNumber)

	if roomOutput.BackgroundImage != nil {
		render.UpdateTextureADT(renderDef.BackgroundImageEntity.TextureId, roomOutput.BackgroundImage)
		// Camera image mask depends on updated camera position
		cameraMasks := mainGameRender.RenderRoom.CameraMaskData[gameDef.CameraId]
		renderDef.CameraMaskEntity.UpdateCameraImageMaskEntity(renderDef, roomOutput, cameraMasks)
	}

	// Update camera switch zones
	cameraSwitchHandler := gameDef.GameRoom.CameraSwitchHandler
	mainGameRender.CameraSwitchDebugEntity = render.NewCameraSwitchDebugEntity(gameDef.CameraId,
		cameraSwitchHandler.CameraSwitches, cameraSwitchHandler.CameraSwitchTransitions)
}

func runGameLoop(mainGameStateInput *MainGameStateInput, gameStateManager *GameStateManager) {
	gameDef := mainGameStateInput.GameDef
	scriptDef := mainGameStateInput.ScriptDef
	mainGameRender := mainGameStateInput.MainGameRender
	renderDef := mainGameRender.RenderDef
	playerEntity := mainGameRender.PlayerEntity

	timeElapsedSeconds := windowHandler.GetTimeSinceLastFrame()
	// Only render these entities for debugging
	debugEntitiesRender := render.DebugEntities{
		CameraSwitchDebugEntity: mainGameRender.CameraSwitchDebugEntity,
		DebugEntities:           mainGameRender.DebugEntities,
	}
	// Update screen
	playerEntity.UpdatePlayerEntity(gameDef.Player, gameDef.Player.PoseNumber)

	renderDef.RenderFrame(*playerEntity, debugEntitiesRender, timeElapsedSeconds)

	handleMainGameInput(gameDef, timeElapsedSeconds, gameDef.GameRoom.CollisionEntities, gameStateManager)
	gameDef.HandleCameraSwitch(gameDef.Player.Position)
	gameDef.HandleRoomSwitch(gameDef.Player.Position)
	aot := gameDef.AotManager.GetAotTriggerNearPlayer(gameDef.Player.Position)
	if aot != nil {
		if aot.Header.Id == game.AOT_EVENT {
			threadNum := aot.Data[0]
			eventNum := aot.Data[3]
			lineData := []byte{fileio.OP_EVT_EXEC, threadNum, 0, eventNum}
			scriptDef.ScriptEvtExec(lineData, gameDef.GameRoom.RoomScriptData)
		}
	}

	scriptDef.RunScript(gameDef.GameRoom.RoomScriptData, timeElapsedSeconds, gameDef, renderDef)
}

func handleMainGameInput(gameDef *game.GameDef,
	timeElapsedSeconds float64,
	collisionEntities []fileio.CollisionEntity,
	gameStateManager *GameStateManager) {
	if windowHandler.InputHandler.IsActive(client.PLAYER_FORWARD) {
		gameDef.HandlePlayerInputForward(collisionEntities, timeElapsedSeconds)
	}

	if windowHandler.InputHandler.IsActive(client.PLAYER_BACKWARD) {
		gameDef.HandlePlayerInputBackward(collisionEntities, timeElapsedSeconds)
	}

	if !windowHandler.InputHandler.IsActive(client.PLAYER_FORWARD) &&
		!windowHandler.InputHandler.IsActive(client.PLAYER_BACKWARD) {
		gameDef.Player.PoseNumber = -1
	}

	if windowHandler.InputHandler.IsActive(client.PLAYER_ROTATE_LEFT) {
		gameDef.RotatePlayerLeft(timeElapsedSeconds)
	}

	if windowHandler.InputHandler.IsActive(client.PLAYER_ROTATE_RIGHT) {
		gameDef.RotatePlayerRight(timeElapsedSeconds)
	}

	if windowHandler.InputHandler.IsActive(client.ACTION_BUTTON) {
		if gameStateManager.CanUpdateGameState() {
			gameDef.HandlePlayerActionButton(collisionEntities)
			gameStateManager.UpdateLastTimeChangeState()
		}
	}

	if windowHandler.InputHandler.IsActive(client.PLAYER_VIEW_INVENTORY) {
		if gameStateManager.CanUpdateGameState() {
			gameStateManager.UpdateGameState(GAME_STATE_INVENTORY)
			gameStateManager.UpdateLastTimeChangeState()
		}
	}
}
