export class UrlShortenError extends Error {
  readonly status: number;
  readonly response?: unknown;

  constructor(status: number, message: string, response?: unknown) {
    super(message);
    this.name = "UrlShortenError";
    this.status = status;
    this.response = response;
  }
}

export class UnauthorizedError extends UrlShortenError {
  constructor(message = "Unauthorized") {
    super(401, message);
    this.name = "UnauthorizedError";
  }
}

export class ForbiddenError extends UrlShortenError {
  constructor(message = "Forbidden") {
    super(403, message);
    this.name = "ForbiddenError";
  }
}

export class NotFoundError extends UrlShortenError {
  constructor(message = "Not found") {
    super(404, message);
    this.name = "NotFoundError";
  }
}

export class ConflictError extends UrlShortenError {
  constructor(message = "Conflict") {
    super(409, message);
    this.name = "ConflictError";
  }
}

export class GoneError extends UrlShortenError {
  constructor(message = "Gone") {
    super(410, message);
    this.name = "GoneError";
  }
}

export class NetworkError extends Error {
  readonly cause?: unknown;
  constructor(message: string, cause?: unknown) {
    super(message);
    this.name = "NetworkError";
    this.cause = cause;
  }
}

export class TimeoutError extends Error {
  constructor(message = "request timed out") {
    super(message);
    this.name = "TimeoutError";
  }
}
