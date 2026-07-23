import { mount } from "svelte";
import "./app.css";
import App from "./App.svelte";
import { appSettings } from "./lib/app-settings.svelte";

appSettings.load().then(() => {
  mount(App, { target: document.getElementById("app")! });
});
