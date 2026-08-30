export function syncPackageVersionContent(current, version) {
  const packageJson = JSON.parse(current);
  if (packageJson.version === version) {
    return current;
  }

  packageJson.version = version;
  const newline = current.includes("\r\n") ? "\r\n" : "\n";
  const trailingNewline = current.endsWith("\n") ? newline : "";
  return `${JSON.stringify(packageJson, null, 2).replaceAll("\n", newline)}${trailingNewline}`;
}
