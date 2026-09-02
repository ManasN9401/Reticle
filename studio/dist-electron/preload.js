import { contextBridge, ipcRenderer } from "electron";
//#region electron/preload.ts
contextBridge.exposeInMainWorld("electronAPI", {
	minimize: () => ipcRenderer.send("window-minimize"),
	maximize: () => ipcRenderer.send("window-maximize"),
	close: () => ipcRenderer.send("window-close")
});
//#endregion
export {};
