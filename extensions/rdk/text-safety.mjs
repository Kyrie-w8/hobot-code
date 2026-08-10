export function toWellFormedText(value) {
  const text = String(value ?? "");
  let result = "";

  for (let index = 0; index < text.length; index += 1) {
    const codeUnit = text.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      const next = text.charCodeAt(index + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        result += text[index] + text[index + 1];
        index += 1;
      } else {
        result += "\uFFFD";
      }
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) {
      result += "\uFFFD";
    } else {
      result += text[index];
    }
  }

  return result;
}
