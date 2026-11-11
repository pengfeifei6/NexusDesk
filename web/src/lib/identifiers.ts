// Keep BIGINT identities as strings through validation and lossless encoding.
export function positiveID(value: string): boolean {
  return /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n
}
