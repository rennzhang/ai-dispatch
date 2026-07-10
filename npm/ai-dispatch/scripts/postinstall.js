"use strict";

const { installBinary } = require("../lib/install");

if (process.env.AI_DISPATCH_SKIP_DOWNLOAD === "1") {
  console.log("ai-dispatch: skipping native binary download (AI_DISPATCH_SKIP_DOWNLOAD=1)");
} else {
  installBinary()
    .then(({ filename }) => {
      console.log(`ai-dispatch: installed verified ${filename}`);
    })
    .catch((error) => {
      console.error(`ai-dispatch: installation failed: ${error.message}`);
      process.exitCode = 1;
    });
}
