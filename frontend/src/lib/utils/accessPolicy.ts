/** Built-in NDMS access policy (Policy0..PolicyN). */
export function isStandardAccessPolicyName(name: string): boolean {
	return /^Policy\d+$/.test(name);
}

/** HydraRoute Neo / other subsystem policy (custom NDMS name). */
export function isHydraRouteAccessPolicy(policy: { name: string; isStandard?: boolean }): boolean {
	return policy.isStandard === false || !isStandardAccessPolicyName(policy.name);
}
