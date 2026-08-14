function jsonError(label) {
  throw new Error(`${label} is not valid strict JSON`);
}

export function parseStrictJSON(text, label = "JSON document") {
  text = String(text);
  let index = 0;
  const whitespace = () => {
    while (index < text.length && /[\u0020\u000a\u000d\u0009]/u.test(text[index])) index += 1;
  };
  const string = () => {
    if (text[index] !== '"') jsonError(label);
    const start = index++;
    while (index < text.length) {
      const current = text[index++];
      if (current === '"') {
        try {
          return JSON.parse(text.slice(start, index));
        } catch {
          jsonError(label);
        }
      }
      if (current === "\\") {
        if (index >= text.length) jsonError(label);
        if (text[index] === "u") index += 5;
        else index += 1;
      }
    }
    jsonError(label);
  };
  const value = () => {
    whitespace();
    if (text[index] === "{") return object();
    if (text[index] === "[") return array();
    if (text[index] === '"') {
      string();
      return;
    }
    const start = index;
    while (index < text.length && !/[\u0020\u000a\u000d\u0009,\]}]/u.test(text[index])) index += 1;
    if (start === index) jsonError(label);
    try {
      JSON.parse(text.slice(start, index));
    } catch {
      jsonError(label);
    }
  };
  const object = () => {
    index += 1;
    whitespace();
    const keys = new Set();
    if (text[index] === "}") {
      index += 1;
      return;
    }
    while (index < text.length) {
      whitespace();
      const key = string();
      if (keys.has(key)) throw new Error(`${label} contains a duplicate key: ${key}`);
      keys.add(key);
      whitespace();
      if (text[index++] !== ":") jsonError(label);
      value();
      whitespace();
      if (text[index] === "}") {
        index += 1;
        return;
      }
      if (text[index++] !== ",") jsonError(label);
    }
    jsonError(label);
  };
  const array = () => {
    index += 1;
    whitespace();
    if (text[index] === "]") {
      index += 1;
      return;
    }
    while (index < text.length) {
      value();
      whitespace();
      if (text[index] === "]") {
        index += 1;
        return;
      }
      if (text[index++] !== ",") jsonError(label);
    }
    jsonError(label);
  };
  value();
  whitespace();
  if (index !== text.length) jsonError(label);
  try {
    return JSON.parse(text);
  } catch {
    jsonError(label);
  }
}
