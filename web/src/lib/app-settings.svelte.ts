import { api } from "./api";

class AppSettingsStore {
  collapseThinking = $state(true);

  async load(): Promise<void> {
    try {
      this.collapseThinking = (await api.settings()).collapseThinking;
    } catch {
      // Keep the safe default when settings cannot be loaded.
    }
  }

  async setCollapseThinking(collapseThinking: boolean): Promise<void> {
    const settings = await api.setSettings({ collapseThinking });
    this.collapseThinking = settings.collapseThinking;
  }
}

export const appSettings = new AppSettingsStore();
