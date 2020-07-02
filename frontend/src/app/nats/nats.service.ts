import { Injectable } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';

import { Observable, throwError } from 'rxjs';
import { catchError } from 'rxjs/operators';

export interface Connz {
  server_id: string;
  num_connections: number;
}

export interface Varz {
  server_id: string;
  server_name: string;
  version: string;
  git_commit: string;
  go: string;
  proto: number;
  host: string;
  port: number;
}

@Injectable()
export class NATSService {
  constructor(private http: HttpClient) { }

  public list(): Observable<string[]> {
    return this.http.get<string[]>('/api/list');
  }

  public varz(name: string): Observable<Varz> {
    return this.http.get<Varz>('/api/varz', {
      params: {
        name,
      }
    });
  }

  public connz(name: string, limit: number = 0, offset: number = 0): Observable<Connz> {
    return this.http.get<Connz>('/api/connz', {
      params: {
        name,
        limit: limit.toString(),
        offset: offset.toString()
      }
    });
  }
}
