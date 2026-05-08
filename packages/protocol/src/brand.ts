declare const brand: unique symbol;

export type Brand<T, B extends string> = T & { readonly [brand]: B };
