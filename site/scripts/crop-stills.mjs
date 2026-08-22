#!/usr/bin/env node
// Crops vhs stills to their content plus one line of padding, in place.
//   node scripts/crop-stills.mjs still-*.png
import sharp from "sharp";

const BACKGROUND = { r: 14, g: 15, b: 19 };
const PAD = 68; // the 36px frame padding plus one 32px line

class CropError extends Error {
  constructor(message, context) {
    super(message);
    this.name = "CropError";
    this.context = context;
  }
}

for (const file of process.argv.slice(2)) {
  const trimmed = await sharp(file).trim({ background: BACKGROUND, threshold: 12 }).toBuffer();
  const { width, height } = await sharp(trimmed).metadata();
  if (!width || !height) throw new CropError("nothing left after trim", { file });
  await sharp(trimmed)
    .extend({ top: PAD, bottom: PAD, left: PAD, right: PAD, background: BACKGROUND })
    .png()
    .toFile(file);
  console.log(`${file}: ${width + 2 * PAD}x${height + 2 * PAD}`);
}
