import { render } from "preact";
import { App } from "./App";
import { globalStyles } from "./styles/theme";

const style = document.createElement("style");
style.textContent = globalStyles;
document.head.appendChild(style);

render(<App />, document.getElementById("root")!);
