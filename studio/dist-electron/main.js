import { BrowserWindow, app, ipcMain } from "electron";
import path from "path";
import { fileURLToPath } from "url";
//#region electron/main.ts
var __filename = fileURLToPath(import.meta.url);
var __dirname = path.dirname(__filename);
var mainWindow = null;
function createWindow() {
	mainWindow = new BrowserWindow({
		width: 1400,
		height: 900,
		titleBarStyle: "hidden",
		frame: false,
		webPreferences: {
			nodeIntegration: true,
			contextIsolation: true,
			preload: path.join(__dirname, "preload.mjs")
		}
	});
	ipcMain.on("window-minimize", () => {
		mainWindow?.minimize();
	});
	ipcMain.on("window-maximize", () => {
		if (mainWindow?.isMaximized()) mainWindow?.unmaximize();
		else mainWindow?.maximize();
	});
	ipcMain.on("window-close", () => {
		mainWindow?.close();
	});
	if (process.env.VITE_DEV_SERVER_URL) {
		mainWindow.loadURL(process.env.VITE_DEV_SERVER_URL);
		mainWindow.webContents.openDevTools();
	} else mainWindow.loadFile(path.join(__dirname, "../dist/index.html"));
	mainWindow.on("closed", () => {
		mainWindow = null;
	});
}
app.whenReady().then(createWindow);
app.on("window-all-closed", () => {
	if (process.platform !== "darwin") app.quit();
});
app.on("activate", () => {
	if (mainWindow === null) createWindow();
});
//#endregion
export {};
